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
	"github.com/soypat/seqs/eth/ntp"
	"github.com/soypat/seqs/stacks"
)

//go:embed wifi_creds.json
var wifiCreds []byte

type wifiConfig struct {
	SSID      string `json:"ssid"`
	Password  string `json:"password"`
	Hostname  string `json:"hostname"`
	NTPServer string `json:"ntpServer"`
	// UTCOffsetMinutes shifts the NTP-synced UTC time to local time. The
	// device has no way to sense timezone or DST, so this must be set
	// explicitly (e.g. 600 for UTC+10, -300 for UTC-5).
	UTCOffsetMinutes int `json:"utcOffsetMinutes"`
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

// joinWithRetry attempts to join the given SSID up to attempts times. The
// CYW43439's WPA2 4-way handshake occasionally times out (observed on
// hardware as rxEvent PSK_SUP status=4 reason=15) for reasons unrelated to
// configuration (AP/RF timing); a short retry with backoff resolves it
// without masking a genuinely wrong SSID/password, which fails identically
// on every attempt.
func joinWithRetry(dev *cyw43439.Device, ssid string, opts cyw43439.JoinOptions, attempts int) error {
	var err error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			println("wifi: join attempt", i+1, "of", attempts)
			time.Sleep(time.Duration(i) * time.Second)
		}
		if err = dev.Join(ssid, opts); err == nil {
			return nil
		}
		println("wifi: join failed:", err.Error())
	}
	return err
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
		joinOpts = cyw43439.JoinOptions{}
	}
	if err := joinWithRetry(dev, cfg.SSID, joinOpts, 3); err != nil {
		return time.Time{}, fmt.Errorf("join wifi: %w", err)
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

	gateway := dhcpClient.Router()
	if !gateway.IsValid() {
		return time.Time{}, errors.New("DHCP did not provide a router/gateway address")
	}
	println("arp: resolving gateway", gateway.String())
	if err := stack.ARP().BeginResolve(gateway); err != nil {
		return time.Time{}, fmt.Errorf("arp begin: %w", err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for !stack.ARP().IsDone() && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	_, gatewayHW, err := stack.ARP().ResultAs6()
	if err != nil {
		return time.Time{}, fmt.Errorf("arp resolve gateway %s: %w", gateway, err)
	}
	println("arp: gateway resolved")

	ntpAddr, err := netip.ParseAddr(cfg.NTPServer)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse NTP server %q: %w", cfg.NTPServer, err)
	}
	if !ntpAddr.Is4() {
		return time.Time{}, fmt.Errorf("NTP server %q is not IPv4", cfg.NTPServer)
	}

	// The NTP server lives beyond the local network, so Ethernet frames
	// must be addressed to the gateway's hardware address, not our own
	// and not the (unreachable-via-ARP) NTP server's address.
	println("ntp: requesting time from", ntpAddr.String())
	ntpClient := stacks.NewNTPClient(stack, 12345)
	if err := ntpClient.BeginDefaultRequest(gatewayHW, ntpAddr); err != nil {
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

	// ntpClient.Offset() is a duration measured relative to the NTP
	// client's own internal zero-based clock (which started counting from
	// the NTP epoch, 1900-01-01), not relative to time.Now()'s epoch. Adding
	// it to ntp.BaseTime() reconstructs the true UTC calendar time; adding
	// it to time.Now() (different epoch/reference) produces a nonsensical
	// date roughly 126 years in the future.
	now := ntp.BaseTime().Add(ntpClient.Offset())
	// The device cannot sense timezone or DST, so shift by the configured
	// fixed UTC offset to get local time for display.
	now = now.Add(time.Duration(cfg.UTCOffsetMinutes) * time.Minute)
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
