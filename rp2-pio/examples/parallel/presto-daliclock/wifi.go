//go:build rp2350 && rp2350b

package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"machine"
	"net/netip"
	"time"

	"github.com/soypat/cyw43439"
	"github.com/soypat/seqs/eth/dhcp"
	"github.com/soypat/seqs/stacks"
)

//go:embed wifi_creds.json
var wifiCreds []byte

type wifiConfig struct {
	SSID      string `json:"ssid"`
	Password  string `json:"password"`
	Hostname  string `json:"hostname"`
	NTPServer string `json:"ntpServer"`
}

func loadWiFiConfig() (wifiConfig, error) {
	var cfg wifiConfig
	if err := json.Unmarshal(wifiCreds, &cfg); err != nil {
		return cfg, fmt.Errorf("decode wifi config: %w", err)
	}
	cfg.Hostname = firstNonEmpty(cfg.Hostname, "presto-daliclock")
	cfg.NTPServer = firstNonEmpty(cfg.NTPServer, "162.159.200.1")
	if cfg.SSID == "" || cfg.SSID == "YOUR_WIFI_SSID" {
		return cfg, errors.New("wifi config missing SSID")
	}
	return cfg, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func syncClockFromNTP() (time.Time, error) {
	println("wifi: loading config")
	cfg, err := loadWiFiConfig()
	if err != nil {
		return time.Time{}, err
	}
	println("wifi: config loaded, ssid=", cfg.SSID, "ntp=", cfg.NTPServer)

	logger := slog.New(slog.NewTextHandler(machine.Serial, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	devcfg := cyw43439.DefaultWifiConfig()
	devcfg.Logger = logger
	dev := cyw43439.NewPicoWDevice()
	println("wifi: initializing cyw43439")
	if err := dev.Init(devcfg); err != nil {
		return time.Time{}, fmt.Errorf("wifi init: %w", err)
	}
	println("wifi: init done, joining", cfg.SSID)

	joinOpts := cyw43439.JoinOptions{Passphrase: cfg.Password}
	if cfg.Password == "" {
		if err := dev.Join(cfg.SSID, cyw43439.JoinOptions{}); err != nil {
			return time.Time{}, fmt.Errorf("join open wifi: %w", err)
		}
	} else {
		if err := dev.Join(cfg.SSID, joinOpts); err != nil {
			return time.Time{}, fmt.Errorf("join wifi: %w", err)
		}
	}
	println("wifi: joined", cfg.SSID)

	mac, err := dev.HardwareAddr6()
	if err != nil {
		return time.Time{}, fmt.Errorf("read MAC: %w", err)
	}
	println("wifi: mac bytes", mac[0], mac[1], mac[2], mac[3], mac[4], mac[5])
	stack := stacks.NewPortStack(stacks.PortStackConfig{
		MAC:             mac,
		MaxOpenPortsUDP: 4,
		MaxOpenPortsTCP: 1,
		MTU:             1500,
		Logger:          logger,
	})
	dev.RecvEthHandle(stack.RecvEth)
	go nicLoop(dev, stack)

	println("dhcp: requesting lease")
	dhcpClient := stacks.NewDHCPClient(stack, dhcp.DefaultClientPort)
	if err := dhcpClient.BeginRequest(stacks.DHCPRequestConfig{
		Xid:      uint32(time.Now().UnixNano()),
		Hostname: cfg.Hostname,
	}); err != nil {
		return time.Time{}, fmt.Errorf("dhcp begin: %w", err)
	}

	deadline := time.Now().Add(20 * time.Second)
	lastLog := time.Now()
	for dhcpClient.State() != dhcp.StateBound && time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		if time.Since(lastLog) > 2*time.Second {
			println("dhcp: waiting, state=", int(dhcpClient.State()))
			lastLog = time.Now()
		}
	}
	if dhcpClient.State() != dhcp.StateBound {
		return time.Time{}, errors.New("DHCP timeout")
	}
	stack.SetAddr(dhcpClient.Offer())
	println("dhcp: bound, addr=", dhcpClient.Offer().String())

	ntpAddr, err := netip.ParseAddr(cfg.NTPServer)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse NTP server %q: %w", cfg.NTPServer, err)
	}
	if !ntpAddr.Is4() {
		return time.Time{}, fmt.Errorf("NTP server %q is not IPv4", cfg.NTPServer)
	}

	println("ntp: requesting time from", ntpAddr.String())
	ntpClient := stacks.NewNTPClient(stack, 12345)
	if err := ntpClient.BeginDefaultRequest(mac, ntpAddr); err != nil {
		return time.Time{}, fmt.Errorf("ntp request: %w", err)
	}

	deadline = time.Now().Add(10 * time.Second)
	lastLog = time.Now()
	for !ntpClient.IsDone() && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		if time.Since(lastLog) > 2*time.Second {
			println("ntp: waiting for response")
			lastLog = time.Now()
		}
	}
	if !ntpClient.IsDone() {
		return time.Time{}, errors.New("NTP timeout")
	}
	println("ntp: response received, offset=", ntpClient.Offset().String())

	now := time.Now().UTC().Add(ntpClient.Offset())
	return now, nil
}

func nicLoop(dev *cyw43439.Device, stack *stacks.PortStack) {
	const queueSize = 3
	var queue [queueSize][cyw43439.MaxFrameSize]byte
	var lengths [queueSize]int

	for {
		for i := 0; i < 1; i++ {
			gotPacket, err := dev.PollOne()
			if err != nil {
				println("poll error:", err.Error())
			}
			if !gotPacket {
				break
			}
		}

		for i := range queue {
			if lengths[i] != 0 {
				continue
			}
			n, err := stack.HandleEth(queue[i][:])
			if err != nil {
				println("stack error:", err.Error())
				n = 0
			}
			if n == 0 {
				break
			}
			lengths[i] = n
		}

		for i := range queue {
			if lengths[i] == 0 {
				continue
			}
			if err := dev.SendEth(queue[i][:lengths[i]]); err != nil {
				println("send error:", err.Error())
			}
			lengths[i] = 0
		}

		if allZero(lengths[:]) {
			time.Sleep(50 * time.Millisecond)
		}
	}
}

func allZero(v []int) bool {
	for _, n := range v {
		if n != 0 {
			return false
		}
	}
	return true
}
