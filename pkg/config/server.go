package config

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

// ApplyGlobalDefaults applies global server defaults to this instance
// Called for instances that don't specify their own timeout values
func (sic *ServerInstanceConfig) ApplyGlobalDefaults(globals ServersConfig) {
	if sic.ShutdownTimeout == nil && globals.ShutdownTimeout > 0 {
		t := globals.ShutdownTimeout
		sic.ShutdownTimeout = &t
	}
	if sic.DrainTimeout == nil && globals.DrainTimeout > 0 {
		t := globals.DrainTimeout
		sic.DrainTimeout = &t
	}
	if sic.ReadTimeout == nil && globals.ReadTimeout > 0 {
		t := globals.ReadTimeout
		sic.ReadTimeout = &t
	}
	if sic.WriteTimeout == nil && globals.WriteTimeout > 0 {
		t := globals.WriteTimeout
		sic.WriteTimeout = &t
	}
	if sic.IdleTimeout == nil && globals.IdleTimeout > 0 {
		t := globals.IdleTimeout
		sic.IdleTimeout = &t
	}
}

// Validate validates the ServerInstanceConfig
func (sic *ServerInstanceConfig) Validate() error {
	if sic.Name == "" {
		return fmt.Errorf("server instance name cannot be empty")
	}
	if sic.Port <= 0 || sic.Port > 65535 {
		return fmt.Errorf("invalid port: %d (must be 1-65535)", sic.Port)
	}

	// Validate TLS options are mutually exclusive
	tlsCount := 0
	if sic.SSLCert != "" || sic.SSLKey != "" {
		tlsCount++
	}
	if sic.SelfSignedSSL {
		tlsCount++
	}
	if sic.AutoTLS {
		tlsCount++
	}
	if tlsCount > 1 {
		return fmt.Errorf("server '%s': only one TLS option can be enabled", sic.Name)
	}

	// If using certificate files, both must be provided
	if (sic.SSLCert != "" && sic.SSLKey == "") || (sic.SSLCert == "" && sic.SSLKey != "") {
		return fmt.Errorf("server '%s': both ssl_cert and ssl_key must be provided", sic.Name)
	}

	// If using AutoTLS, domains must be specified
	if sic.AutoTLS && len(sic.AutoTLSDomains) == 0 {
		return fmt.Errorf("server '%s': auto_tls_domains must be specified when auto_tls is enabled", sic.Name)
	}

	return nil
}

// Validate validates the ServersConfig
func (sc *ServersConfig) Validate() error {
	if len(sc.Instances) == 0 {
		return fmt.Errorf("at least one server instance must be configured")
	}

	if sc.DefaultServer != "" {
		if _, ok := sc.Instances[sc.DefaultServer]; !ok {
			return fmt.Errorf("default server '%s' not found in instances", sc.DefaultServer)
		}
	}

	// Validate each instance
	for name := range sc.Instances {
		instance := sc.Instances[name]
		if instance.Name != name {
			return fmt.Errorf("server instance name mismatch: key='%s', name='%s'", name, instance.Name)
		}
		if err := instance.Validate(); err != nil {
			return err
		}
	}

	return nil
}

// GetDefault returns the default server instance configuration.
// The returned pointer refers to a copy: mutating it does not modify sc.Instances.
func (sc *ServersConfig) GetDefault() (*ServerInstanceConfig, error) {
	if sc.DefaultServer == "" {
		return nil, fmt.Errorf("no default server configured")
	}

	instance, ok := sc.Instances[sc.DefaultServer]
	if !ok {
		return nil, fmt.Errorf("default server '%s' not found", sc.DefaultServer)
	}

	return &instance, nil
}

// GetIPs returns the hostname, a comma-separated list of non-loopback IPs and the
// same IPs as []net.IP. The lookup is bounded by a short timeout.
func GetIPs() (hostname string, ipList string, ipNetList []net.IP) {
	hostname, _ = os.Hostname()
	ipNetList = make([]net.IP, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var ips []string
	if addrs, err := net.DefaultResolver.LookupIPAddr(ctx, hostname); err == nil {
		for _, a := range addrs {
			if a.IP.IsLoopback() {
				continue
			}
			ips = append(ips, a.IP.String())
			ipNetList = append(ipNetList, a.IP)
		}
	}

	if len(ips) == 0 {
		ifaceAddrs, _ := net.InterfaceAddrs()
		for _, a := range ifaceAddrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.IsLoopback() {
				continue
			}
			ips = append(ips, ipn.IP.String())
			ipNetList = append(ipNetList, ipn.IP)
		}
	}

	return hostname, strings.Join(ips, ","), ipNetList
}
