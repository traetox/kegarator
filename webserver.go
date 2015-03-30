package main

import (
	"net"
	"net/http"
	"encoding/json"
	"errors"
	"sync"
	"time"
	"log"
	ds18b20 "github.com/traetox/goDS18B20"
)

type webserver struct {
	lst net.Listener
	probes *ds18b20.ProbeGroup
	srv    *http.Server
	kt *kegTracker
}

func NewWebserver(bind, serveDir string, probes *ds18b20.ProbeGroup, kt *kegTracker, lg *log.Logger) (*webserver, error) {
	if probes == nil {
		return nil, errors.New("nil probes")
	}
	lst, err := net.Listen("tcp", bind)
	if err != nil {
		return nil, err
	}

	ws := &webserver{
		lst: lst,
		probes: probes,
		kt:   kt,
	}

	mux := http.NewServeMux()
	//setup handlers
	mux.Handle("/", http.FileServer(http.Dir(serveDir)))
	mux.Handle("/api/temps/month", http.HandlerFunc(ws.serveMonthTemps))
	mux.Handle("/api/temps/all", http.HandlerFunc(ws.serveAllTemps))
	mux.Handle("/api/temps/now", http.HandlerFunc(ws.serveCurrentTemps))
	mux.Handle("/api/compressor/all", http.HandlerFunc(ws.serveAllCompressor))
	mux.Handle("/api/compressor/month", http.HandlerFunc(ws.serveMonthCompressor))

	svr := &http.Server{
		ErrorLog: lg,
		Handler:  mux,
	}
	ws.srv=svr
	
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
	temps, err := ws.probes.ReadAlias()
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
