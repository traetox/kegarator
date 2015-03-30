package main

import (
	"code.google.com/p/gcfg"
	"errors"
	"fmt"
	"net"
	"os"
	"time"
)

const (
	defaultBindPort           uint16  = 80
	defaultProbeInterval      uint16  = 5
	defaultTempRecordInterval uint    = 60 * 60 //every hour
	defaultBindIface          string  = ``
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
}

type conf struct {
	port                         uint16
	iface                        string
	wwwdir                       string
	interval                     uint16
	tempRecordInterval           uint
	minTemp, maxTemp, targetTemp float32
	compressorGPIO               uint16
	kegDB                        string
	aliases                      map[string]probes
}

type config struct {
	Global struct {
		Bind_Port                   uint16
		Bind_Interface              string
		WWW_Dir                     string
		Keg_DB                      string
		Temperature_Probe_Interval  uint16
		Temperature_Record_Interval uint
		Minimum_Temperature         float32
		Maximum_Temperature         float32
		Target_Temperature          float32
		Compressor_GPIO             uint16
	}
	Alias map[string]*struct {
		ID                 string
		Compressor_Control bool
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
			amap[k] = probes{v.ID, v.Compressor_Control}
		}
	}
	return &conf{
		port:               cfg.Global.Bind_Port,
		iface:              cfg.Global.Bind_Interface,
		wwwdir:             cfg.Global.WWW_Dir,
		interval:           cfg.Global.Temperature_Probe_Interval,
		tempRecordInterval: cfg.Global.Temperature_Record_Interval,
		minTemp:            cfg.Global.Minimum_Temperature,
		maxTemp:            cfg.Global.Maximum_Temperature,
		targetTemp:         cfg.Global.Target_Temperature,
		compressorGPIO:     cfg.Global.Compressor_GPIO,
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
	c.Global.Bind_Interface = defaultBindIface
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
	return net.JoinHostPort(c.iface, fmt.Sprintf("%d", c.port))
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
