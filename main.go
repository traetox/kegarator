package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gravwell/ingest"
	"github.com/gravwell/ingest/entry"
	ds18b20 "github.com/traetox/goDS18B20"
	gpio "github.com/traetox/goGPIO"
)

var (
	configLocOverride        = flag.String("c", "", "Config file location override")
	verbose                  = flag.Bool("v", false, "Verbose stdout logging")
	compressorControlAliases []string
	lg                       simplelog

	tempTagId uint16 = 0x1200
	compTagId uint16 = 0x1320

	tempFunc tempPacker = binaryTempPack
	compFunc compPacker = binaryCompPack
)

type logger interface {
	Info(string, ...interface{}) error
	Warn(string, ...interface{}) error
	Error(string, ...interface{}) error
}

type tempPacker func(uint16, entry.Timestamp, float32, string) ([]byte, error)
type compPacker func(uint16, entry.Timestamp, entry.Timestamp) ([]byte, error)

func init() {
	flag.Parse()
}

func main() {
	c, err := OpenConfig(*configLocOverride) //open the default location
	if err != nil {
		lg.Error("Failed to open config file: %v", err)
		return
	}
	if err = setupPackers(c.dataFormat); err != nil {
		lg.Error("failed to setup packers: %v", err)
		return
	}

	//open probes and initialize them
	if err := ds18b20.Setup(); err != nil {
		lg.Error("Setup failed: %v", err)
		return
	}
	probes, err := ds18b20.New()
	if err != nil {
		lg.Error("Failed to create new probe group: %v", err)
		return
	}

	//open compressor and initializae it
	compressor, err := gpio.New(int(c.CompressorGPIO()))
	if err != nil {
		lg.Error("Failed to open compressorGPIO: %v", err)
		return
	}
	if err := compressor.SetOutput(); err != nil {
		lg.Error("Failed to set compressor as output: %v", err)
		return
	}
	if err := compressor.Off(); err != nil {
		lg.Error("Failed to initialize the compressor: %v", err)
		return
	}

	aliases := c.Aliases()
	vlog("Starting with probe aliases: %v", aliases)
	for k, v := range aliases {
		vlog("Assigning %s to %s", k, v)
		if err := probes.AssignAlias(k, v); err != nil {
			lg.Error("Probe alias error %s: %v", v, err)
			return
		}
		cc, err := c.AliasCompressorControl(k)
		if err != nil {
			lg.Error("Failed to find alias when testing for compressor control %v", err)
			return
		}

		if cc {
			compressorControlAliases = append(compressorControlAliases, v)
		}
	}
	xx, err := probes.Read()
	vlog("testing: %v: %v", xx, err)
	xx, err = probes.ReadAlias()
	vlog("testing alias: %v: %v", xx, err)
	defer probes.Close()
	if len(compressorControlAliases) == 0 {
		lg.Error("No probes with compressor control have been defined")
		return
	}

	mxConfig, err := c.MuxerConfig()
	if err != nil {
		lg.Error("Failed to acquire ingester config: %v", err)
		os.Exit(-1)
	}

	//open an ingester
	igst, err := ingest.NewMuxer(mxConfig)
	if err != nil {
		lg.Error("Failed to get muxer config: %v", err)
		os.Exit(-1)
	}

	if err := igst.Start(); err != nil {
		lg.Error("Failed to start ingest: %v", err)
		os.Exit(-1)
	}

	if err := igst.WaitForHot(3 * time.Second); err != nil {
		lg.Error("Wait for hot timeout: %v", err) //not fatal
		os.Exit(-1)
	}

	kl := &keglog{
		mx: igst,
	}
	if kl.logtag, err = igst.GetTag(printTag); err != nil {
		lg.Error("Failed to get print tag: %v", err)
		os.Exit(-1)
	}
	if kl.kegtag, err = igst.GetTag(kegTag); err != nil {
		lg.Error("Failed to get temp tag: %v", err)
		os.Exit(-1)
	}
	if kl.comptag, err = igst.GetTag(compTag); err != nil {
		lg.Error("Failed to get compressor tag: %v", err)
		os.Exit(-1)
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
	vlog("Starting temperature recording routine with: %v", probes)
	for {
		temps := make(map[string]float32, 1)
		t, err := probes.ReadAlias()
		if err != nil {
			lg.Error("Failed to read probes: %v", err)
			vlog("Failed to read probes: %v", err)
			time.Sleep(interval)
			continue
		}
		vlog("Read aliases: %v, %v", t, err)
		for k, v := range t {
			temps[k] = v.Celsius()
		}
		if err := kt.AddTemps(temps); err != nil {
			lg.Error("Failed to add temps: %v", err)
			vlog("Failed to Update temperatures: %v", err)
			time.Sleep(interval)
			continue
		}
		vlog("Updated temperatures: %v", temps)
		vlog("sleeping %v", interval)
		time.Sleep(interval)
	}
}

func compressorPanic(compressor *gpio.GPIO) {
	if err := compressor.Off(); err != nil {
		lg.Error("Failed to force compressor off: %v", err)
	} else {
		lg.Error("Forced compressor off")
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
			lg.Info("Update failure: %v", err)
			time.Sleep(c.ProbeInterval())
			continue
		}
		temps, err := probes.Read()
		if err != nil {
			lg.Error("Read failed: %v", err)
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
					lg.Error("Failed to turn off compressor: %v", err)
				}
				break
			}
			probeValues[i] = t.Celsius()
		}
		//check if we are being forced off
		if forceOff {
			if err := compressor.Off(); err != nil {
				lg.Error("Compressor failed to disengage: %v", err)
			}
			if started != nilTime {
				if err := kt.AddCompressor(started, time.Now()); err != nil {
					lg.Error("Failed to track compressor interval: %v", err)
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
					lg.Error("Failed to disengage compressor: %v", err)
				}
				if err := kt.AddCompressor(started, time.Now()); err != nil {
					lg.Error("Failed to track compressor interval: %v", err)
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
					lg.Error("Compressor failed to start: %v", err)
				}
			}
		}
		vlog("Updated compressor data")
		time.Sleep(c.ProbeInterval())
	}
}

type keglog struct {
	mx      *ingest.IngestMuxer
	logtag  entry.EntryTag
	kegtag  entry.EntryTag
	comptag entry.EntryTag
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
		vlog("Failed to get source IP: %v", err)
		return err
	}
	for k, v := range temps {
		ts := entry.Now()
		bts, err := tempFunc(tempTagId, ts, v, k)
		if err != nil {
			return err
		}
		e := entry.Entry{
			TS:   ts,
			Tag:  kl.kegtag,
			SRC:  src,
			Data: bts,
		}
		err = kl.mx.WriteEntry(&e)
		vlog("Sending temperaturer %d %v %v %s: %v", tempTagId, ts, v, k, err)
		if err != nil {
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
		vlog("Failed to get source IP: %v", err)
		return
	}
	diff := e.Sec - s.Sec
	if diff < 0 {
		diff = 0
	}
	var bts []byte
	if bts, err = compFunc(compTagId, s, e); err != nil {
		return
	}
	ent := entry.Entry{
		TS:   entry.Now(),
		SRC:  src,
		Tag:  kl.comptag,
		Data: bts,
	}
	err = kl.mx.WriteEntry(&ent)
	vlog("Sending compressor %d %v %v %v: %v", compTagId, s, e, diff, err)
	return
}

func vlog(f string, args ...interface{}) {
	if !*verbose {
		return
	}
	ln := strings.Trim(fmt.Sprintf(f, args...), "\n\t ")
	fmt.Println(time.Now().Format(time.StampMicro), ln)
}

type simplelog struct{}

func (nl simplelog) Info(f string, args ...interface{}) (err error) {
	_, err = fmt.Fprintf(os.Stdout, f+"\n", args...)
	return
}

func (nl simplelog) Warn(f string, args ...interface{}) (err error) {
	_, err = fmt.Fprintf(os.Stdout, f+"\n", args...)
	return
}

func (nl simplelog) Error(f string, args ...interface{}) (err error) {
	_, err = fmt.Fprintf(os.Stderr, f+"\n", args...)
	return
}

func setupPackers(tp string) (err error) {
	tp = strings.TrimSpace(tp)
	switch tp {
	case `binary`:
		tempFunc = binaryTempPack
		compFunc = binaryCompPack
	case `text`:
		tempFunc = textTempPack
		compFunc = textCompPack
	default:
		err = fmt.Errorf("unknown packing format: %v", tp)
	}
	return
}

func binaryTempPack(tg uint16, ts entry.Timestamp, v float32, name string) (bts []byte, err error) {
	bb := bytes.NewBuffer(nil)
	if err = binary.Write(bb, binary.BigEndian, tempTagId); err != nil {
		return
	}
	if err = binary.Write(bb, binary.BigEndian, ts.Sec); err != nil {
		return
	}
	if err = binary.Write(bb, binary.BigEndian, ts.Nsec); err != nil {
		return
	}
	if err = binary.Write(bb, binary.BigEndian, v); err != nil {
		return
	}
	if _, err = bb.WriteString(name); err != nil {
		return
	}
	bts = bb.Bytes()
	return
}

func textTempPack(tg uint16, ts entry.Timestamp, v float32, name string) (bts []byte, err error) {
	bts = []byte(fmt.Sprintf("%s\t%x\t%f\t%s", ts.String(), tg, v, name))
	return
}

func binaryCompPack(tg uint16, s, e entry.Timestamp) (bts []byte, err error) {
	diff := e.Sec - s.Sec
	if diff < 0 {
		diff = 0
	}
	bb := bytes.NewBuffer(nil)
	if err = binary.Write(bb, binary.BigEndian, tg); err != nil {
		return
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
	bts = bb.Bytes()
	return
}

func textCompPack(tg uint16, s, e entry.Timestamp) (bts []byte, err error) {
	diff := e.Sec - s.Sec
	if diff < 0 {
		diff = 0
	}
	bts = []byte(fmt.Sprintf("%s\t%s\t%x\t%ds", s.String(), e.String(), tg, diff))
	return
}
