package main

import (
	"flag"
	"log"
	"os"
	"sync"
	"time"

	ds18b20 "github.com/traetox/goDS18B20"
	gpio "github.com/traetox/goGPIO"
)

var (
	configLocOverride        = flag.String("c", "", "Config file location override")
	logOverride              = flag.String("l", "", "Log file override")
	compressorControlAliases []string
	logFile                  string = `/var/log/kegarator.log`
	lg                       *log.Logger
)

func init() {
	flag.Parse()
	if *logOverride != "" {
		logFile = *logOverride
	}
}

func main() {
	f, err := os.OpenFile(logFile, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
	if err != nil {
		log.Fatal("Failed to open log file", err)
	}
	lg = log.New(f, "", log.LstdFlags)

	c, err := OpenConfig(*configLocOverride) //open the default location
	if err != nil {
		lg.Println("Failed to open config file: ", err)
		return
	}

	//open probes and initialize them
	if err := ds18b20.Setup(); err != nil {
		lg.Println("Setup failed:", err)
		return
	}
	probes, err := ds18b20.New()
	if err != nil {
		lg.Println("Failed to create new probe group:", err)
		return
	}

	//open compressor and initializae it
	compressor, err := gpio.New(int(c.CompressorGPIO()))
	if err != nil {
		lg.Println("Failed to open compressorGPIO:", err)
		return
	}
	if err := compressor.SetOutput(); err != nil {
		lg.Println("Failed to set compressor as output:", err)
		return
	}
	if err := compressor.Off(); err != nil {
		lg.Println("Failed to initialize the compressor:", err)
		return
	}

	aliases := c.Aliases()
	for k, v := range aliases {
		probes.AssignAlias(k, v)
		cc, err := c.AliasCompressorControl(k)
		if err != nil {
			lg.Println("Failed to find alias when testing for compressor control", err)
			return
		}

		if cc {
			compressorControlAliases = append(compressorControlAliases, v)
		}
	}
	defer probes.Close()
	if len(compressorControlAliases) == 0 {
		lg.Println("No probes with compressor control have been defined")
		return
	}

	//open the keg tracker
	kegTracker, err := NewTracker(c.KegDB())
	if err != nil {
		lg.Println("Failed to create kegTracker", err)
		return
	}
	defer kegTracker.Close()

	//bind for the webserver
	ws, err := NewWebserver(c.Bind(), c.WebDir(), probes, compressor, kegTracker, c, lg)
	if err != nil {
		lg.Println("Failed to create webserver", c.Bind())
		return
	}
	defer ws.Close()
	//fire off the management routine
	wg := sync.WaitGroup{}
	wg.Add(3)
	go manageCompressor(probes, compressor, c, kegTracker, &wg)
	if c.TemperatureRecordInterval() > 0 {
		go recordTemps(c.TemperatureRecordInterval(), probes, kegTracker, &wg)
	}
	go ws.Serve(&wg)
	wg.Wait()
}

func recordTemps(interval time.Duration, probes *ds18b20.ProbeGroup, kt *kegTracker, wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		temps := make(map[string]float32, 1)
		t, err := probes.ReadAlias()
		if err != nil {
			lg.Printf("Failed to read probes: %v\n", err)
			time.Sleep(interval)
			continue
		}
		for k, v := range t {
			temps[k] = v.Celsius()
		}
		kt.AddTemps(time.Now(), temps)
		time.Sleep(interval)
	}
}

func compressorPanic(compressor *gpio.GPIO) {
	if err := compressor.Off(); err != nil {
		lg.Printf("Failed to force compressor off: %v\n", err)
	} else {
		lg.Printf("Forced compressor off\n")
	}
}

func manageCompressor(probes *ds18b20.ProbeGroup, compressor *gpio.GPIO, c *conf, kt *kegTracker, wg *sync.WaitGroup) {
	defer compressorPanic(compressor) //ensure that compressor always goes off on our way out
	defer wg.Done()
	min, max, target := c.TemperatureRange()
	probeValues := make([]float32, len(compressorControlAliases))
	nilTime := time.Time{}
	var started time.Time
	started = nilTime //i know this should initialize to the zero time... but i am anal
	for {
		//update temps
		if err := probes.Update(); err != nil {
			lg.Printf("Update failure: %v\n", err)
			return
		}
		temps, err := probes.Read()
		if err != nil {
			lg.Printf("Read failed: %v\n", err)
			return
		}
		//check temperatures and set compressor
		forceOff := false

		for i := range compressorControlAliases {
			t := temps[compressorControlAliases[i]]
			if t.Celsius() < min {
				forceOff = true //going off is always priority
				if err := compressor.Off(); err != nil {
					log.Println("Failed to turn off compressor", err)
				}
				break
			}
			probeValues[i] = t.Celsius()
		}
		//check if we are being forced off
		if forceOff {
			if err := compressor.Off(); err != nil {
				lg.Println("Compressor failed to disengage", err)
			}
			if started != nilTime {
				if err := kt.AddCompressor(started, time.Now()); err != nil {
					lg.Println("Failed to track compressor interval", err)
				}
			}
			started = time.Time{}
			continue
		}
		if compressor.State() {
			allInsideRange := true
			//loop through and see what we should do
			for i := range probeValues {
				if probeValues[i] > max || probeValues[i] < target {
					allInsideRange = false
				}
			}
			if allInsideRange {
				//everybody is good
				if err := compressor.Off(); err != nil {
					lg.Println("Failed to disengage compressor", err)
				}
				if err := kt.AddCompressor(started, time.Now()); err != nil {
					lg.Println("Failed to track compressor interval", err)
				}
				started = nilTime
			}
			//at least one probe outside the range, so leave the compressor on
		} else {
			allInsideRange := true
			//compressor is off, determine if we should turn on
			for i := range probeValues {
				if probeValues[i] > max {
					allInsideRange = false
				}
			}
			if !allInsideRange {
				started = time.Now()
				if err := compressor.On(); err != nil {
					lg.Println("Compressor failed to start: ", err)
				}
			}
		}
		time.Sleep(c.ProbeInterval())
	}
}
