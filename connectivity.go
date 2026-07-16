/*
 *
 * Module:    bibtex_check_dev
 * Package:   Main
 * Component: Connectivity
 *
 * Internet connectivity detection. Online is set once at startup by probing
 * a known host. All network-dependent code should gate on Online.
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 16.07.2026
 *
 */

package main

import (
	"net"
	"net/http"
	"time"
)

const (
	onlineProbeHost = "8.8.8.8:53"                                        // Google public DNS — TCP reachability pre-check
	onlineProbeHTTP = "http://connectivitycheck.gstatic.com/generate_204" // returns 204 on real internet; captive portals return 200/302
	dialTimeout     = 3 * time.Second
)

// Online is set once at startup: true when general internet connectivity is available.
var Online bool

func init() {
	Online = probeConnectivity()
}

// probeConnectivity performs a two-stage check: TCP dial to confirm basic IP
// reachability, then HTTP GET to distinguish captive portals from real internet.
func probeConnectivity() bool {
	conn, err := net.DialTimeout("tcp", onlineProbeHost, dialTimeout)
	if err != nil {
		return false
	}
	conn.Close()
	// TCP dial can succeed behind captive portals (intercept and complete handshake).
	// A real HTTP probe distinguishes "WiFi with no internet" from actual connectivity.
	client := &http.Client{
		Timeout: dialTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse // don't follow captive-portal redirects
		},
	}
	resp, err := client.Get(onlineProbeHTTP)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusNoContent
}
