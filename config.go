package main

import (
	"code.google.com/p/gcfg"
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"time"
)

const (
	defaultBindPort           uint16  = 80
	defaultProbeInterval      uint16  = 5
	defaultCompressorMinTime  uint16  = 60      //compressor should stay on for 60 seconds
	defaultTempRecordInterval uint    = 60 * 60 //every hour
	defaultBindAddress        string  = ``
	defaultWwwDir             string  = `/opt/kegarator`
	defaultMinTempC           float32 = 0.0
	defaultMaxTempC           float32 = 6.0
	defaultTargetTempC        float32 = 5.0
	defaultKegDB              string  = `/etc/kegarator.db`
	defaultCompressorGPIO     uint16  = 22

	defaultConfigFile = `/etc/kegarator.conf`
	maxConfSize       = 1024 * 1024
)

type probes struct {
	alias             string
	compressorControl bool
	minOverride       float32
	maxOverride       float32
}

type conf struct {
	port                         uint16
	addr                         string
	wwwdir                       string
	interval                     uint16
	tempRecordInterval           uint
	minTemp, maxTemp, targetTemp float32
	compressorGPIO               uint16
	compressorOnTime             uint16
	powerRate                    float32 //in cents per KW/h
	compressorDraw               float32 //in watts
	kegDB                        string
	aliases                      map[string]probes
}

type config struct {
	Global struct {
		Bind_Port                   uint16
		Bind_Address                string
		WWW_Dir                     string
		Keg_DB                      string
		Temperature_Probe_Interval  uint16
		Temperature_Record_Interval uint
		Minimum_Temperature         float32
		Maximum_Temperature         float32
		Target_Temperature          float32
		Compressor_GPIO             uint16
		Compressor_Power_Draw       float32
		Power_Rate                  float32
	}
	Alias map[string]*struct {
		ID                 string
		Compressor_Control bool
		Min_Override       float32
		Max_Override       float32
	}
}

func OpenConfig(confFile string) (*conf, error) {
	if confFile == "" {
		confFile = defaultConfigFile
	}
	fin, err := os.Open(confFile)
	if err != nil {
		return nil, err
	}
	fi, err := fin.Stat()
	if err != nil {
		return nil, err
	}
	defer fin.Close()
	if fi.Size() > maxConfSize {
		return nil, errors.New("Config file too large")
	}
	bb := make([]byte, fi.Size())
	tot := 0
	for int64(tot) < fi.Size() {
		n, err := fin.Read(bb[tot:])
		if err != nil {
			return nil, err
		}
		tot += n
	}
	var cfg config
	if err := gcfg.ReadStringInto(&cfg, string(bb)); err != nil {
		return nil, err
	}
	amap := make(map[string]probes, 1)
	for k, v := range cfg.Alias {
		if k != "" && v.ID != "" {
			_, ok := amap[k]
			if ok {
				return nil, errors.New("Duplicate alias")
			}
			amap[k] = probes{
				alias:             v.ID,
				compressorControl: v.Compressor_Control,
				minOverride:       v.Min_Override,
				maxOverride:       v.Max_Override,
			}
		}
	}
	return &conf{
		port:               cfg.Global.Bind_Port,
		addr:               cfg.Global.Bind_Address,
		wwwdir:             cfg.Global.WWW_Dir,
		interval:           cfg.Global.Temperature_Probe_Interval,
		tempRecordInterval: cfg.Global.Temperature_Record_Interval,
		minTemp:            cfg.Global.Minimum_Temperature,
		maxTemp:            cfg.Global.Maximum_Temperature,
		targetTemp:         cfg.Global.Target_Temperature,
		compressorGPIO:     cfg.Global.Compressor_GPIO,
		compressorDraw:     cfg.Global.Compressor_Power_Draw,
		powerRate:          cfg.Global.Power_Rate,
		kegDB:              cfg.Global.Keg_DB,

		aliases: amap,
	}, nil
}

func prepopulate(c *config) error {
	if c == nil {
		return errors.New("Invalid config pointer")
	}
	c.Global.WWW_Dir = defaultWwwDir
	c.Global.Bind_Port = defaultBindPort
	c.Global.Bind_Address = defaultBindAddress
	c.Global.Temperature_Probe_Interval = defaultProbeInterval
	c.Global.Minimum_Temperature = defaultMinTempC
	c.Global.Maximum_Temperature = defaultMaxTempC
	c.Global.Target_Temperature = defaultTargetTempC
	c.Global.Compressor_GPIO = defaultCompressorGPIO
	c.Global.Keg_DB = defaultKegDB

	c.Alias = nil
	return nil
}

func (c conf) WebDir() string {
	return c.wwwdir
}

func (c conf) Bind() string {
	return net.JoinHostPort(c.addr, fmt.Sprintf("%d", c.port))
}

func (c conf) Aliases() map[string]string {
	ret := make(map[string]string, 1)
	for k, v := range c.aliases {
		ret[k] = v.alias
	}
	return ret
}

func (c conf) CompressorGPIO() uint16 {
	return c.compressorGPIO
}

func (c conf) TemperatureRange() (float32, float32, float32) {
	return c.minTemp, c.maxTemp, c.targetTemp
}

func (c conf) ProbeInterval() time.Duration {
	return time.Duration(c.interval) * time.Second
}

func (c conf) TemperatureRecordInterval() time.Duration {
	return time.Duration(c.tempRecordInterval) * time.Second
}

func (c conf) CompressorMinOnTime() time.Duration {
	return time.Duration(c.compressorOnTime) * time.Second
}

func (c conf) PowerRate() float32 {
	return c.powerRate
}

func (c conf) CompressorPowerDraw() float32 {
	return c.compressorDraw
}

func (c conf) AliasCompressorControl(alias string) (bool, error) {
	p, ok := c.aliases[alias]
	if !ok {
		return false, errors.New("Alias not found")
	}
	return p.compressorControl, nil
}

func (c conf) KegDB() string {
	return c.kegDB
}

type probeDesc struct {
	ID                string
	Alias             string
	CompressorControl bool
	MinOverride       float32
	MaxOverride       float32
}
type probeDescL []probeDesc

func (c conf) ProbeList() []probeDesc {
	var pd []probeDesc
	for k, v := range c.aliases {
		pd = append(pd, probeDesc{
			ID:                v.alias,
			Alias:             k,
			CompressorControl: v.compressorControl,
			MinOverride:       v.minOverride,
			MaxOverride:       v.maxOverride,
		})
	}
	sort.Sort(probeDescL(pd))
	return pd
}

func (p probeDescL) Less(i, j int) bool {
	//compressor control probes always have priority
	if p[i].CompressorControl && !p[j].CompressorControl {
		return true
	} else if p[j].CompressorControl && !p[i].CompressorControl {
		return false
	}

	//if compressor control is equivelent, sort on alias name
	return p[i].Alias < p[j].Alias
}
func (p probeDescL) Len() int      { return len(p) }
func (p probeDescL) Swap(i, j int) { p[i], p[j] = p[j], p[i] }
