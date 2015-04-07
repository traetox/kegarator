package main

import (
	"encoding/json"
	"errors"
	ds18b20 "github.com/traetox/goDS18B20"
	gpio "github.com/traetox/goGPIO"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

type webserver struct {
	lst        net.Listener
	probes     *ds18b20.ProbeGroup
	compressor *gpio.GPIO
	srv        *http.Server
	kt         *kegTracker
	cfg        *conf
}

func NewWebserver(bind, serveDir string, probes *ds18b20.ProbeGroup, compressor *gpio.GPIO, kt *kegTracker, c *conf, lg *log.Logger) (*webserver, error) {
	if probes == nil {
		return nil, errors.New("nil probes")
	}
	lst, err := net.Listen("tcp", bind)
	if err != nil {
		return nil, err
	}

	ws := &webserver{
		lst:        lst,
		probes:     probes,
		compressor: compressor,
		kt:         kt,
		cfg:        c,
	}

	mux := http.NewServeMux()
	//setup handlers
	mux.Handle("/", http.FileServer(http.Dir(serveDir)))
	mux.Handle("/api/temps/month", http.HandlerFunc(ws.serveMonthTemps))
	mux.Handle("/api/temps/all", http.HandlerFunc(ws.serveAllTemps))
	mux.Handle("/api/temps/now", http.HandlerFunc(ws.serveCurrentTemps))
	mux.Handle("/api/compressor/now", http.HandlerFunc(ws.serveCompressorNow))
	mux.Handle("/api/compressor/all", http.HandlerFunc(ws.serveAllCompressor))
	mux.Handle("/api/compressor/month", http.HandlerFunc(ws.serveMonthCompressor))
	mux.Handle("/api/config", http.HandlerFunc(ws.serveConfig))

	svr := &http.Server{
		ErrorLog: lg,
		Handler:  mux,
	}
	ws.srv = svr

	return ws, nil
}

func (ws *webserver) Serve(wg *sync.WaitGroup) {
	ws.srv.Serve(ws.lst)
}

func (ws *webserver) Close() error {
	return ws.lst.Close()
}

func (ws *webserver) serveAllTemps(w http.ResponseWriter, r *http.Request) {
	jenc := json.NewEncoder(w)
	temps, err := ws.kt.GetAllTemps()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := jenc.Encode(temps); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}
func (ws *webserver) serveMonthTemps(w http.ResponseWriter, r *http.Request) {
	jenc := json.NewEncoder(w)
	temps, err := ws.kt.GetTemps(time.Now())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := jenc.Encode(temps); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (ws *webserver) serveCurrentTemps(w http.ResponseWriter, r *http.Request) {
	jenc := json.NewEncoder(w)
	temps, err := ws.probes.Read()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := jenc.Encode(temps); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (ws *webserver) serveAllCompressor(w http.ResponseWriter, r *http.Request) {
	jenc := json.NewEncoder(w)
	comps, err := ws.kt.GetAllCompressor()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := jenc.Encode(comps); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (ws *webserver) serveCompressorNow(w http.ResponseWriter, r *http.Request) {
	jenc := json.NewEncoder(w)
	w.Header().Set("Content-Type", "application/json")
	if err := jenc.Encode(ws.compressor.State()); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (ws *webserver) serveMonthCompressor(w http.ResponseWriter, r *http.Request) {
	jenc := json.NewEncoder(w)
	comps, err := ws.kt.GetCompressor(time.Now())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := jenc.Encode(comps); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

type kegConfig struct {
	HighTemp       float32
	LowTemp        float32
	TargetTemp     float32
	ProbeInterval  uint
	RecordInterval uint
	Probes         []probeDesc
}

func (ws *webserver) serveConfig(w http.ResponseWriter, r *http.Request) {
	jenc := json.NewEncoder(w)
	low, high, target := ws.cfg.TemperatureRange()
	probeInt := uint(ws.cfg.ProbeInterval().Seconds())
	recordInt := uint(ws.cfg.TemperatureRecordInterval())
	pbs := ws.cfg.ProbeList()
	kc := kegConfig{
		HighTemp:       high,
		LowTemp:        low,
		TargetTemp:     target,
		ProbeInterval:  probeInt,
		RecordInterval: recordInt,
		Probes:         pbs,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := jenc.Encode(kc); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}
