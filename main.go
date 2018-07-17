package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/gravwell/ingest"
	"github.com/gravwell/ingest/entry"
	ds18b20 "github.com/traetox/goDS18B20"
	gpio "github.com/traetox/goGPIO"
)

var (
	configLocOverride        = flag.String("c", "", "Config file location override")
	logOverride              = flag.String("l", "", "Log file override")
	compressorControlAliases []string
	logFile                  string = `/var/log/kegarator.log`
	lg                       *log.Logger

	tempTagId uint16 = 0x1200
	compTagId uint16 = 0x1320
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

	mxConfig, err := c.MuxerConfig()
	if err != nil {
		log.Fatal("Failed to acquire ingester config", err)
	}

	//open an ingester
	igst, err := ingest.NewMuxer(mxConfig)
	if err != nil {
		log.Fatal("Failed to get muxer config", err)
	}

	if err := igst.Start(); err != nil {
		log.Fatal("Failed to start ingest")
	}

	if err := igst.WaitForHot(3 * time.Second); err != nil {
		log.Println("Wait for hot timeout", err) //not fatal
	}

	kl := &keglog{
		mx: igst,
	}
	if kl.logtag, err = igst.GetTag(printTag); err != nil {
		log.Fatal("Failed to get print tag", err)
	}
	if kl.kegtag, err = igst.GetTag(kegTag); err != nil {
		log.Fatal("Failed to get temp tag", err)
	}

	//fire off the management routine
	wg := sync.WaitGroup{}
	wg.Add(2)
	go manageCompressor(probes, compressor, c, kl, &wg)
	go recordTemps(c.TemperatureRecordInterval(), probes, kl, &wg)
	wg.Wait()
}

func recordTemps(interval time.Duration, probes *ds18b20.ProbeGroup, kt *keglog, wg *sync.WaitGroup) {
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
		if err := kt.AddTemps(temps); err != nil {
			lg.Printf("Failed to add temps: %v\n", err)
		}
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

func manageCompressor(probes *ds18b20.ProbeGroup, compressor *gpio.GPIO, c *conf, kt *keglog, wg *sync.WaitGroup) {
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
			time.Sleep(c.ProbeInterval())
			continue
		}
		temps, err := probes.Read()
		if err != nil {
			lg.Printf("Read failed: %v\n", err)
			time.Sleep(c.ProbeInterval())
			continue
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
			//we want to make sure the compressor is on long enough to actually do something
			if time.Since(started).Seconds() < c.CompressorMinOnTime().Seconds() {
				allInsideRange = false
			}
			//loop through and see what we should do
			for i := range probeValues {
				//we keep the compressor on if one of the probes is showing a warm value
				if probeValues[i] > target {
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

type keglog struct {
	mx     *ingest.IngestMuxer
	logtag entry.EntryTag
	kegtag entry.EntryTag
}

func (kl keglog) Printf(arg string, args ...interface{}) error {
	src, err := kl.mx.SourceIP()
	if err != nil {
		return err
	}
	s := fmt.Sprintf(arg, args...)
	e := entry.Entry{
		TS:   entry.Now(),
		Tag:  kl.logtag,
		SRC:  src,
		Data: []byte(s),
	}
	return kl.mx.WriteEntry(&e)
}

func (kl keglog) AddTemps(temps map[string]float32) error {
	src, err := kl.mx.SourceIP()
	if err != nil {
		fmt.Println("Failed to get source ip", err)
		return err
	}
	for k, v := range temps {
		bb := bytes.NewBuffer(nil)
		ts := entry.Now()
		if err := binary.Write(bb, binary.BigEndian, tempTagId); err != nil {
			return err
		}
		if err := binary.Write(bb, binary.BigEndian, ts.Sec); err != nil {
			return err
		}
		if err := binary.Write(bb, binary.BigEndian, ts.Nsec); err != nil {
			return err
		}
		if err := binary.Write(bb, binary.BigEndian, v); err != nil {
			return err
		}
		if _, err := bb.WriteString(k); err != nil {
			return err
		}
		e := entry.Entry{
			TS:   ts,
			Tag:  kl.kegtag,
			SRC:  src,
			Data: bb.Bytes(),
		}
		if err := kl.mx.WriteEntry(&e); err != nil {
			return err
		}
	}
	return nil
}

func (kl keglog) AddCompressor(start, stop time.Time) (err error) {
	s := entry.FromStandard(start)
	e := entry.FromStandard(stop)
	var src net.IP
	if src, err = kl.mx.SourceIP(); err != nil {
		return
	}
	diff := e.Sec - s.Sec
	if diff < 0 {
		diff = 0
	}
	bb := bytes.NewBuffer(nil)
	if err := binary.Write(bb, binary.BigEndian, compTagId); err != nil {
		return err
	}
	//encode start, stop, then timeone
	if err = binary.Write(bb, binary.BigEndian, s); err != nil {
		return
	}
	if err = binary.Write(bb, binary.BigEndian, e); err != nil {
		return
	}
	if err = binary.Write(bb, binary.BigEndian, diff); err != nil {
		return
	}
	ent := entry.Entry{
		TS:   entry.Now(),
		SRC:  src,
		Tag:  kl.kegtag,
		Data: bb.Bytes(),
	}
	err = kl.mx.WriteEntry(&ent)
	return
}
