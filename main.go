package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"strings"
	_ "sync"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
	pinger "github.com/raintank/go-pinger"
)

var (
	count     int
	timeout   time.Duration
	interval  time.Duration
	ipVersion string

	google = net.ParseIP("8.8.8.8")

	GlobalPinger *pinger.Pinger
)

type PingResult struct {
	Loss   *float64 `json:"loss"`
	Min    *float64 `json:"min"`
	Max    *float64 `json:"max"`
	Avg    *float64 `json:"avg"`
	Median *float64 `json:"median"`
	Mdev   *float64 `json:"mdev"`
	Error  *string  `json:"error"`
}

type RaintankProbePing struct {
	Hostname  string        `json:"hostname"`
	Timeout   time.Duration `json:"timeout"`
	IPVersion string        `json:"ipversion"`
}



func main() {

	flag.IntVar(&count, "count", 1, "number of pings to sent to each host")
	flag.DurationVar(&timeout, "timeout", time.Second*1, "timeout time before pings are assumed lost")
	flag.DurationVar(&interval, "interval", time.Second*20, "frequency at which the pings should be sent to hosts.")
	flag.StringVar(&ipVersion, "ipversion", "v4", "ipversion to use.")

	flag.Parse()
	if (flag.NArg() == 0) || (flag.NArg() > 20) {
		log.Fatal("no hosts specified or your exced the limit permit of 20 hosts")
	}
	hosts := flag.Args()
	var err error

	GlobalPinger, err = pinger.NewPinger("ipv4", 1000)

	if err != nil {
		log.Fatal(err)
	}

	client := influxdb2.NewClient("http://INFLUX_IP:8086", "api:api")
	writeAPI := client.WriteAPIBlocking("", "icmp/autogen")

	GlobalPinger.Start()
	defer GlobalPinger.Stop()

	//GlobalPinger.Debug = true

	ticker := time.NewTicker(interval)

	for range ticker.C {
		for _, host := range hosts {
			go ping(host, writeAPI)
		}
	}
}

func ResolveHost(host, ipversion string) (string, error) {
	addrs, err := net.LookupHost(host)
	if err != nil || len(addrs) < 1 {
		return "", fmt.Errorf("failed to resolve hostname to IP")
	}

	for _, addr := range addrs {
		if ipversion == "any" {
			return addr, nil
		}

		if strings.Contains(addr, ":") || strings.Contains(addr, "%") {
			if ipversion == "v6" {
				return addr, nil
			}
		} else {
			if ipversion == "v4" {
				return addr, nil
			}
		}
	}

	return "", fmt.Errorf("failed to resolve hostname to valid IP")
}

func ping(host string, influxApi api.WriteAPIBlocking) {
	addr, err := ResolveHost(host, ipVersion)
	if err != nil {
		log.Println(err)
		return
	}
	stats, err := GlobalPinger.Ping(net.ParseIP(addr), count, timeout)
	if err != nil {
		log.Println(err)
		return
	}

	total := time.Duration(0)
	min := timeout
	max := time.Duration(0)
	for _, t := range stats.Latency {
		total += t
		if t < min {
			min = t
		}
		if t > max {
			max = t
		}
	}
	avg := time.Duration(0)
	if total > 0 {
		avg = total / time.Duration(stats.Received)
	}

	log.Printf("%s sent=%d  received=%d  avg=%s min=%s max=%s\n", host, stats.Sent, stats.Received, avg.String(), min.String(), max.String())

	line := fmt.Sprintf("rtt,host=%s,stats_received=%d,stats_sent=%d avg=%d", host, stats.Received, stats.Sent, int64(avg))
	err = influxApi.WriteRecord(context.Background(), line)
	if err != nil {
		fmt.Println("Error writing the metric in the database!")
		return
	}
}
