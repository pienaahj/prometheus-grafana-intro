package main

import (
	"log"
	"net/http"
	"strconv"
	"strings"

	"encoding/json"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Device struct {
	ID       int    `json:"id"`
	Mac      string `json:"mac"`
	Firmware string `json:"firmware"`
}

type metrics struct {
	devices prometheus.Gauge
	// add any metadata you want to add to the metrics here in key, value pairs
	info     *prometheus.GaugeVec
	upgrades *prometheus.GaugeVec
}

func NewMetrics(req prometheus.Registerer) *metrics {
	m := &metrics{
		devices: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "intro", // your app name note: it's picky about this format siggle word needed
			Name:      "connected_devices",
			Help:      "Number of currently connected devices",
		}),
		info: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "intro",
			Name:      "info",
			Help:      "Information about the Intro App environment.",
		}, []string{"version"}),
		upgrades: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "intro",
			Name:      "device_upgrade_total",
			Help:      "Number of upgrade devices.",
		}, []string{"type"}),
	}
	req.MustRegister(m.devices, m.info, m.upgrades) // register the metrics with the registry
	return m                                        // return a pointer to the metrics object
}

var dvs []Device
var version string

func init() {
	version = "1.0.0" // set the version of the app in real app read from os or env vars
	dvs = []Device{
		{1, "5f-33-CC-1F-43-82", "2.1.6"},
		{2, "EF-2B-C4-F5-D6-34", "2.1.6"},
	}
}

func main() {
	reg := prometheus.NewRegistry() // create a non global registery
	//reg.MustRegister(collectors.NewGoCollector())             // register the go collector
	m := NewMetrics(reg)                                      // create a metrics object
	m.devices.Set(float64(len(dvs)))                          // set the value of the metric to number of connected devices
	m.info.With(prometheus.Labels{"version": version}).Set(1) // set the value of the metric to 1)

	// create two servers one for the prometheus metrics and one for the devices
	// the prometheus metrics server will be used to expose the metrics to prometheus
	// the devices server will be used to expose the devices to the outside world
	dMux := http.NewServeMux()
	rdh := registerDevicesHandler{metrics: m}
	dMux.Handle("/devices", rdh)

	pMux := http.NewServeMux()
	// promHandler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}) // custom prometheus handler
	promHandler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{}) // custom prometheus handler
	pMux.Handle("/metrics", promHandler)

	go func() {
		log.Fatal(http.ListenAndServe(":8080", dMux))
	}()

	go func() {
		log.Fatal(http.ListenAndServe(":8081", pMux))
	}()

	select {} // this blocks until the program is terminated
	// http.Handle("/metrics", promHandler)
	// http.HandleFunc("/devices", getDevices)
	// http.ListenAndServe(":8081", nil)
}

type registerDevicesHandler struct {
	metrics *metrics
}

func (rdh registerDevicesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		getDevices(w, r)
	case "POST":
		createDevice(w, r, rdh.metrics)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
}

func getDevices(w http.ResponseWriter, r *http.Request) {
	b, err := json.Marshal(dvs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(b)
}

func createDevice(w http.ResponseWriter, r *http.Request, m *metrics) {
	var dv Device
	err := json.NewDecoder(r.Body).Decode(&dv)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	dvs = append(dvs, dv)
	// add the device to the list of devices
	m.devices.Set(float64(len(dvs)))
	w.WriteHeader(http.StatusCreated)
	w.Write([]byte("Device created"))
}

func upgradeDevice(w http.ResponseWriter, r *http.Request, m *metrics) {
	path := strings.TrimPrefix(r.URL.Path, "/devices/")

	id, err := strconv.Atoi(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var dv Device
	err = json.NewDecoder(r.Body).Decode(&dv)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	for i := range dvs {
		if dvs[i].ID == id {
			dvs[i].Firmware = dv.Firmware
		}
	}
	m.upgrades.With(prometheus.Labels{"type": "router"}).Inc()
	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte("Upgrading..."))
}
