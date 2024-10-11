package main

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"encoding/json"

	"math/rand"

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
	info          *prometheus.GaugeVec
	upgrades      *prometheus.CounterVec
	duration      *prometheus.HistogramVec
	loginDuration prometheus.Summary
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
		upgrades: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "intro",
			Name:      "device_upgrade_total",
			Help:      "Number of upgrade devices.",
		}, []string{"type"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "intro",
			Name:      "request_duration_seconds", // note: you should use seconds
			Help:      "Duration of the request.",
			// 4 time larger for apdex score
			// Buckets: prometheus.ExponentialBuckets(0.1, 1.5, 5),
			// Buckets: prometheus.LinearBuckets(0.1, 0.1, 5),
			// generate buckets for 0.1, 0.15, 0.2, 0.25, 0.3 these are in seconds
			// these are used to count requests duration that fall into each bucket
			Buckets: []float64{0.1, 0.15, 0.2, 0.25, 0.3}, // these buckets can be a challenge to set correctly
		}, []string{"status", "method"}),
		loginDuration: prometheus.NewSummary(prometheus.SummaryOpts{
			Namespace:  "intro",
			Name:       "login_request_duration_seconds", // note: you should use seconds
			Help:       "Duration of the login request.",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		}), // these are used to calculate the apdex score
	}
	req.MustRegister(m.devices, m.info, m.upgrades, m.duration, m.loginDuration) // register the metrics with the registry
	return m                                                                     // return a pointer to the metrics object
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
	mdh := manageDevicesHandler{metrics: m}

	lh := loginHandler{}
	mlh := middleware(lh, m)

	dMux.Handle("/devices", rdh)
	dMux.Handle("/devices/", mdh)
	// this is a middleware that will be used to log the request
	dMux.Handle("/login", mlh)

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
		getDevices(w, r, rdh.metrics)
	case "POST":
		createDevice(w, r, rdh.metrics)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
}

func getDevices(w http.ResponseWriter, r *http.Request, m *metrics) {
	// get the current time
	now := time.Now()

	b, err := json.Marshal(dvs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// sleep for 200 milliseconds to simulate latency
	sleep(200)

	m.duration.With(prometheus.Labels{"status": "200", "method": "GET"}).Observe(time.Since(now).Seconds())

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
	sleep(1000)

	m.upgrades.With(prometheus.Labels{"type": "router"}).Inc()
	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte("Upgrading..."))
}

type manageDevicesHandler struct {
	metrics *metrics
}

func (mdh manageDevicesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "PUT":
		upgradeDevice(w, r, mdh.metrics)
	default:
		w.Header().Set("Allow", "PUT")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
}

func sleep(ms int) {
	// rand.Seed(time.Now().UnixNano())
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	now := time.Now()
	n := r.Intn(ms + now.Second())
	time.Sleep(time.Duration(n) * time.Millisecond)
}

type loginHandler struct{}

func (l loginHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sleep(200)
	w.Write([]byte("Welcome to the Intro App!"))
}

// middelware to log the request
func middleware(next http.Handler, m *metrics) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		next.ServeHTTP(w, r)
		m.loginDuration.Observe(time.Since(now).Seconds())
		log.Printf("Request %s %s %s %s", r.Method, r.URL.Path, r.RemoteAddr, time.Since(now))
	})
}
