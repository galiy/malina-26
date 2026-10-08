package sysinfo

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Host struct {
	Hostname  string  `json:"hostname"`
	Kernel    string  `json:"kernel"`
	OS        string  `json:"os"`
	Arch      string  `json:"arch"`
	Model     string  `json:"model"`
	Uptime    string  `json:"uptime"`
	UptimeSec float64 `json:"uptime_sec"`
	BootTime  string  `json:"boot_time"`
	Time      string  `json:"time"`
	Timezone  string  `json:"timezone"`
}

type CPU struct {
	Model    string  `json:"model"`
	Cores    int     `json:"cores"`
	Load1    float64 `json:"load1"`
	Load5    float64 `json:"load5"`
	Load15   float64 `json:"load15"`
	UsagePct float64 `json:"usage_percent"`
	FreqKHz  int64   `json:"freq_khz"`
	TempC    float64 `json:"temp_c"`
}

type Memory struct {
	TotalKB     uint64 `json:"total_kb"`
	UsedKB      uint64 `json:"used_kb"`
	FreeKB      uint64 `json:"free_kb"`
	AvailableKB uint64 `json:"available_kb"`
	BuffersKB   uint64 `json:"buffers_kb"`
	CachedKB    uint64 `json:"cached_kb"`
	SwapTotalKB uint64 `json:"swap_total_kb"`
	SwapUsedKB  uint64 `json:"swap_used_kb"`
}

type Disk struct {
	Mount   string  `json:"mount"`
	TotalKB uint64  `json:"total_kb"`
	UsedKB  uint64  `json:"used_kb"`
	FreeKB  uint64  `json:"free_kb"`
	UsedPct float64 `json:"used_percent"`
}

type Iface struct {
	Name    string   `json:"name"`
	MAC     string   `json:"mac"`
	State   string   `json:"state"`
	MTU     int      `json:"mtu"`
	Speed   string   `json:"speed"`
	Addrs   []string `json:"addrs"`
	RxBytes uint64   `json:"rx_bytes"`
	TxBytes uint64   `json:"tx_bytes"`
	RxBps   uint64   `json:"rx_bps"`
	TxBps   uint64   `json:"tx_bps"`
}

type Network struct {
	Interfaces []Iface  `json:"interfaces"`
	Gateway    string   `json:"gateway"`
	DNS        []string `json:"dns"`
}

type Power struct {
	Throttled string   `json:"throttled"`
	Volts     string   `json:"volts"`
	Flags     []string `json:"flags"`      // расшифровка get_throttled (коды)
	MemArmKB  uint64   `json:"mem_arm_kb"` // память ARM
	MemGpuKB  uint64   `json:"mem_gpu_kb"` // память GPU
}

// WifiClient — подключённый к точке доступа Wi-Fi клиент.
type WifiClient struct {
	MAC      string `json:"mac"`
	IP       string `json:"ip"`
	Host     string `json:"host"`
	LeaseEnd int64  `json:"lease_end"`
	Signal   int    `json:"signal"` // dBm, 0 если неизвестно
	RxRate   string `json:"rx_rate"`
	TxRate   string `json:"tx_rate"`
	Online   bool   `json:"online"`
}

// USBDevice — USB-устройство.
type USBDevice struct {
	Bus  string `json:"bus"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Proc — процесс для списка на странице информации.
type Proc struct {
	PID     int     `json:"pid"`
	User    string  `json:"user"`
	CPUPct  float64 `json:"cpu"`
	MemPct  float64 `json:"mem"`
	RSSKB   uint64  `json:"rss_kb"`
	Command string  `json:"cmd"`
}

// Connected — подключённые устройства (Wi-Fi-клиенты AP и USB).
type Connected struct {
	Wifi []WifiClient `json:"wifi"`
	USB  []USBDevice  `json:"usb"`
}

type Info struct {
	Host      Host      `json:"host"`
	CPU       CPU       `json:"cpu"`
	Memory    Memory    `json:"memory"`
	Disks     []Disk    `json:"disks"`
	Net       Network   `json:"network"`
	Power     Power     `json:"power"`
	Connected Connected `json:"connected"`
	Errors    []string  `json:"errors"`
	Failed    []string  `json:"failed_units"`
	TS        int64     `json:"ts"`
}

type cpuTimes struct{ idle, total uint64 }
type netTimes struct{ rx, tx uint64 }

// Sampler кэширует статические данные и прошлые замеры для скоростей.
type Sampler struct {
	mu sync.Mutex

	hostname string
	kernel   string
	osName   string
	arch     string
	model    string
	cpuModel string
	cpuCores int

	lastCPU cpuTimes
	lastNet map[string]netTimes
	lastAt  time.Time

	powerCache   Power
	powerCacheAt time.Time
	failedCache  []string
	failedAt     time.Time

	connMu    sync.Mutex
	connCache Connected

	errMu    sync.Mutex
	errs     map[string]time.Time
	killFlag bool
}

func New() *Sampler {
	s := &Sampler{lastNet: make(map[string]netTimes)}
	s.hostname, _ = os.Hostname()
	s.kernel = readFileTrim("/proc/sys/kernel/osrelease")
	s.osName = osReleaseName()
	s.arch = unameM()
	s.model = strings.TrimRight(readFileTrim("/proc/device-tree/model"), "\x00")
	s.cpuModel, s.cpuCores = cpuInfo()
	if s.arch == "" {
		s.arch = runtime.GOARCH
	}
	s.errs = map[string]time.Time{}
	go s.connLoop()
	return s
}

// Snapshot собирает текущие значения.
func (s *Sampler) Snapshot() Info {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	up := procUptime()

	info := Info{TS: now.Unix()}
	info.Host = Host{
		Hostname:  s.hostname,
		Kernel:    s.kernel,
		OS:        s.osName,
		Arch:      s.arch,
		Model:     s.model,
		Uptime:    formatUptime(up),
		UptimeSec: up,
		BootTime:  now.Add(-time.Duration(up * float64(time.Second))).Format("2006-01-02 15:04:05"),
		Time:      now.Format("2006-01-02 15:04:05"),
		Timezone:  now.Format("MST"),
	}

	l1, l5, l15 := procLoad()
	info.CPU = CPU{
		Model:   s.cpuModel,
		Cores:   s.cpuCores,
		Load1:   l1,
		Load5:   l5,
		Load15:  l15,
		FreqKHz: freqKHz(),
		TempC:   tempC(),
	}
	ct := procCPUTimes()
	if s.lastAt.IsZero() {
		info.CPU.UsagePct = 0
	} else if dTotal := ct.total - s.lastCPU.total; dTotal > 0 {
		dIdle := ct.idle - s.lastCPU.idle
		info.CPU.UsagePct = round1(float64(dTotal-dIdle) / float64(dTotal) * 100)
	}
	s.lastCPU = ct

	info.Memory = memInfo()
	info.Disks = disks()
	info.Net = s.network(now)
	s.lastAt = now

	info.Power = s.power(now)
	info.Connected = s.connected(now)
	info.Failed = s.failedUnits(now)
	if info.Failed == nil {
		info.Failed = []string{}
	}
	info.Errors = s.currentErrs()
	if info.Errors == nil {
		info.Errors = []string{}
	}
	if info.Connected.Wifi == nil {
		info.Connected.Wifi = []WifiClient{}
	}
	if info.Connected.USB == nil {
		info.Connected.USB = []USBDevice{}
	}
	if info.Disks == nil {
		info.Disks = []Disk{}
	}
	if info.Net.DNS == nil {
		info.Net.DNS = []string{}
	}
	if info.Net.Interfaces == nil {
		info.Net.Interfaces = []Iface{}
	}
	return info
}

func (s *Sampler) network(now time.Time) Network {
	var n Network
	n.DNS = dnsServers()
	n.Gateway = defaultGateway()

	nd := netDev()
	ifaces, _ := ifacesList()

	elapsed := now.Sub(s.lastAt).Seconds()
	for i := range ifaces {
		if t, ok := nd[ifaces[i].Name]; ok {
			ifaces[i].RxBytes = t.rx
			ifaces[i].TxBytes = t.tx
			if prev, ok := s.lastNet[ifaces[i].Name]; ok && elapsed > 0 {
				if t.rx >= prev.rx {
					ifaces[i].RxBps = uint64(float64(t.rx-prev.rx) / elapsed)
				}
				if t.tx >= prev.tx {
					ifaces[i].TxBps = uint64(float64(t.tx-prev.tx) / elapsed)
				}
			}
		}
	}
	s.lastNet = nd
	sort.Slice(ifaces, func(i, j int) bool { return ifaces[i].Name < ifaces[j].Name })
	n.Interfaces = ifaces
	return n
}

func round1(f float64) float64 { return float64(int(f*10+0.5)) / 10 }

func formatUptime(sec float64) string {
	d := time.Duration(sec * float64(time.Second))
	days := int(d.Hours()) / 24
	h := int(d.Hours()) % 24
	m := int(d.Minutes()) % 60
	parts := ""
	if days > 0 {
		parts += strconv.Itoa(days) + "d "
	}
	parts += strconv.Itoa(h) + "h " + strconv.Itoa(m) + "m"
	return parts
}

func readFileTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.TrimRight(string(b), "\x00"))
}

func osReleaseName() string {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "PRETTY_NAME=") {
			v := strings.TrimPrefix(sc.Text(), "PRETTY_NAME=")
			return strings.Trim(v, "\"")
		}
	}
	return ""
}

func unameM() string {
	out, err := exec.Command("uname", "-m").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func cpuInfo() (string, int) {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return "", 0
	}
	defer f.Close()
	model := ""
	cores := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "processor"):
			cores++
		case strings.HasPrefix(line, "model name"):
			if model == "" {
				model = strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
			}
		case strings.HasPrefix(line, "Model"):
			if model == "" {
				model = strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
			}
		case strings.HasPrefix(line, "Hardware"):
			if model == "" {
				model = strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
			}
		}
	}
	return model, cores
}

func procUptime() float64 {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(b))
	if len(fields) < 1 {
		return 0
	}
	v, _ := strconv.ParseFloat(fields[0], 64)
	return v
}

func procLoad() (float64, float64, float64) {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0
	}
	f := strings.Fields(string(b))
	if len(f) < 3 {
		return 0, 0, 0
	}
	a, _ := strconv.ParseFloat(f[0], 64)
	c, _ := strconv.ParseFloat(f[1], 64)
	d, _ := strconv.ParseFloat(f[2], 64)
	return a, c, d
}

func procCPUTimes() cpuTimes {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuTimes{}
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)[1:]
		var total, idle uint64
		for i, v := range fields {
			n, _ := strconv.ParseUint(v, 10, 64)
			total += n
			if i == 3 || i == 4 { // idle, iowait
				idle += n
			}
		}
		return cpuTimes{idle: idle, total: total}
	}
	return cpuTimes{}
}

func memInfo() Memory {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return Memory{}
	}
	defer f.Close()
	m := map[string]uint64{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		v, _ := strconv.ParseUint(fields[1], 10, 64)
		m[key] = v
	}
	var mem Memory
	mem.TotalKB = m["MemTotal"]
	mem.FreeKB = m["MemFree"]
	mem.AvailableKB = m["MemAvailable"]
	mem.BuffersKB = m["Buffers"]
	mem.CachedKB = m["Cached"]
	mem.SwapTotalKB = m["SwapTotal"]
	swapFree := m["SwapFree"]
	if mem.SwapTotalKB >= swapFree {
		mem.SwapUsedKB = mem.SwapTotalKB - swapFree
	}
	if mem.TotalKB >= mem.AvailableKB {
		mem.UsedKB = mem.TotalKB - mem.AvailableKB
	}
	return mem
}

func disks() []Disk {
	mounts := []string{"/", "/malina", "/boot/firmware"}
	var out []Disk
	for _, m := range mounts {
		var st syscall.Statfs_t
		if err := syscall.Statfs(m, &st); err != nil {
			continue
		}
		bs := uint64(st.Bsize)
		total := st.Blocks * bs / 1024
		free := st.Bavail * bs / 1024
		used := total - st.Bfree*bs/1024
		pct := 0.0
		if total > 0 {
			pct = round1(float64(used) / float64(total) * 100)
		}
		out = append(out, Disk{Mount: m, TotalKB: total, UsedKB: used, FreeKB: free, UsedPct: pct})
	}
	return out
}

func netDev() map[string]netTimes {
	out := map[string]netTimes{}
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	first := true
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if first {
			first = false
			continue
		}
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		name := strings.TrimSpace(line[:idx])
		fields := strings.Fields(line[idx+1:])
		if len(fields) < 16 {
			continue
		}
		rx, _ := strconv.ParseUint(fields[0], 10, 64)
		tx, _ := strconv.ParseUint(fields[8], 10, 64)
		out[name] = netTimes{rx: rx, tx: tx}
	}
	return out
}

func ifacesList() ([]Iface, error) {
	list, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var out []Iface
	for _, in := range list {
		iface := Iface{Name: in.Name, MTU: in.MTU, MAC: in.HardwareAddr.String()}
		if in.Flags&net.FlagUp != 0 {
			iface.State = "up"
		} else {
			iface.State = "down"
		}
		addrs, _ := in.Addrs()
		for _, a := range addrs {
			iface.Addrs = append(iface.Addrs, a.String())
		}
		sort.Strings(iface.Addrs)
		iface.Speed = ifaceSpeed(in.Name)
		out = append(out, iface)
	}
	return out, nil
}

func ifaceSpeed(name string) string {
	b, err := os.ReadFile("/sys/class/net/" + name + "/speed")
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(string(b))
	if v == "" || v == "-1" {
		return ""
	}
	return v + " Mb/s"
}

func tempC() float64 {
	b, err := os.ReadFile("/sys/class/thermal/thermal_zone0/temp")
	if err != nil {
		return 0
	}
	v, _ := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
	return round1(v / 1000)
}

func freqKHz() int64 {
	b, err := os.ReadFile("/sys/devices/system/cpu/cpu0/cpufreq/scaling_cur_freq")
	if err != nil {
		return 0
	}
	v, _ := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	return v
}

func defaultGateway() string {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	first := true
	for sc.Scan() {
		if first {
			first = false
			continue
		}
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 || fields[1] != "00000000" {
			continue
		}
		gw, err := strconv.ParseUint(fields[2], 16, 32)
		if err != nil {
			continue
		}
		b := []byte{byte(gw), byte(gw >> 8), byte(gw >> 16), byte(gw >> 24)}
		return strconv.Itoa(int(b[0])) + "." + strconv.Itoa(int(b[1])) + "." +
			strconv.Itoa(int(b[2])) + "." + strconv.Itoa(int(b[3]))
	}
	return ""
}

func dnsServers() []string {
	f, err := os.Open("/etc/resolv.conf")
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 2 && fields[0] == "nameserver" {
			out = append(out, fields[1])
		}
	}
	return out
}

// run — единая обёртка для внешних команд: лимит 1 с; при превышении процесс
// убивается, событие логируется и возвращается ошибка (для показа в UI).
func (s *Sampler) run(name string, args ...string) (string, error) {
	const to = time.Second
	ctx, cancel := context.WithTimeout(context.Background(), to)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if ctx.Err() == context.DeadlineExceeded {
		e := fmt.Errorf("%s %s: превышен лимит %s — процесс убит", name, strings.Join(args, " "), to)
		s.noteErr(e.Error())
		s.errMu.Lock()
		s.killFlag = true
		s.errMu.Unlock()
		return "", e
	}
	if err != nil {
		e := fmt.Errorf("%s %s: %v", name, strings.Join(args, " "), err)
		s.noteErr(e.Error())
		return "", e
	}
	return strings.TrimSpace(string(out)), nil
}

func (s *Sampler) noteErr(msg string) {
	s.errMu.Lock()
	if s.errs == nil {
		s.errs = map[string]time.Time{}
	}
	s.errs[msg] = time.Now()
	s.errMu.Unlock()
}

// currentErrs — недавние (за 5 минут) ошибки внешних команд для показа в UI.
func (s *Sampler) currentErrs() []string {
	s.errMu.Lock()
	defer s.errMu.Unlock()
	cut := time.Now().Add(-5 * time.Minute)
	var out []string
	for m, t := range s.errs {
		if t.After(cut) {
			out = append(out, m)
		} else {
			delete(s.errs, m)
		}
	}
	sort.Strings(out)
	return out
}

func (s *Sampler) power(now time.Time) Power {
	if now.Sub(s.powerCacheAt) < 5*time.Second {
		return s.powerCache
	}
	var p Power
	if raw, err := s.run("vcgencmd", "get_throttled"); err == nil {
		p.Throttled = raw
		p.Flags = decodeThrottled(raw)
	}
	if raw, err := s.run("vcgencmd", "measure_volts"); err == nil {
		p.Volts = raw
	}
	p.MemArmKB = s.vcgencmdMem("arm")
	p.MemGpuKB = s.vcgencmdMem("gpu")
	s.powerCache = p
	s.powerCacheAt = now
	return p
}

// decodeThrottled разбирает "throttled=0x..." в список кодов состояний.
func decodeThrottled(raw string) []string {
	i := strings.Index(raw, "0x")
	if i < 0 {
		return nil
	}
	v, err := strconv.ParseUint(strings.TrimSpace(raw[i+2:]), 16, 32)
	if err != nil {
		return nil
	}
	type bf struct {
		bit  uint
		code string
	}
	defs := []bf{
		{0, "undervolt_now"}, {1, "freq_capped_now"}, {2, "throttled_now"}, {3, "soft_temp_now"},
		{16, "undervolt_occ"}, {17, "freq_capped_occ"}, {18, "throttled_occ"}, {19, "soft_temp_occ"},
	}
	var out []string
	for _, d := range defs {
		if v&(1<<d.bit) != 0 {
			out = append(out, d.code)
		}
	}
	return out
}

// vcgencmdMem читает "arm=948M"/"gpu=76M" -> КБ.
func (s *Sampler) vcgencmdMem(which string) uint64 {
	out, err := s.run("vcgencmd", "get_mem", which) // arm=948M
	if err != nil {
		return 0
	}
	eq := strings.Index(out, "=")
	if eq < 0 {
		return 0
	}
	v := strings.TrimSpace(out[eq+1:])
	v = strings.TrimSuffix(v, "M")
	v = strings.TrimSuffix(v, "K")
	n, _ := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
	return n * 1024
}

func (s *Sampler) connected(now time.Time) Connected {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	return s.connCache
}

// connLoop опрашивает устройства в фоне: `iw ... station dump` может зависать,
// поэтому держим это вне Snapshot (чтобы не блокировать /api/system).
func (s *Sampler) connLoop() {
	for {
		c := Connected{Wifi: s.wifiClients(), USB: s.usbDevices()}
		s.connMu.Lock()
		s.connCache = c
		s.connMu.Unlock()
		time.Sleep(15 * time.Second)
	}
}

// wifiClients — клиенты AP: аренды dnsmasq + живые станции (сигнал/битрейты).
func (s *Sampler) wifiClients() []WifiClient {
	// Онлайн определяем по ARP-таблице (файл /proc/net/arp, без внешних команд):
	// в AP-режиме на brcmfmac и `iw`, и `hostapd_cli` могут зависать.
	online := map[string]bool{}
	if b, err := os.ReadFile("/proc/net/arp"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			// IPaddress HWtype Flags HWaddress Mask Device
			if len(f) >= 4 && f[2] == "0x2" {
				online[strings.ToLower(f[3])] = true
			}
		}
	}

	var res []WifiClient
	seen := map[string]bool{}
	for _, leasePath := range []string{"/run/malina-net/dnsmasq.leases", "/var/lib/misc/dnsmasq.leases"} {
		b, err := os.ReadFile(leasePath)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) < 3 {
				continue
			}
			exp, _ := strconv.ParseInt(f[0], 10, 64)
			mac := strings.ToLower(f[1])
			if seen[mac] {
				continue
			}
			seen[mac] = true
			host := ""
			if len(f) >= 4 {
				host = f[3]
			}
			wc := WifiClient{MAC: mac, IP: f[2], Host: host, LeaseEnd: exp}
			if online[mac] {
				wc.Online = true
			}
			res = append(res, wc)
		}
		break
	}
	sort.Slice(res, func(i, j int) bool {
		if res[i].Online != res[j].Online {
			return res[i].Online
		}
		return res[i].IP < res[j].IP
	})
	return res
}

// usbDevices — список USB-устройств (lsusb).
func (s *Sampler) usbDevices() []USBDevice {
	out, _ := s.run("lsusb")
	var res []USBDevice
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Bus ") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 6 || f[4] != "ID" {
			continue
		}
		name := strings.Join(f[6:], " ")
		res = append(res, USBDevice{Bus: f[1], ID: f[5], Name: name})
	}
	return res
}

// Processes — снимок процессов (для отдельного эндпоинта /api/processes).
func (s *Sampler) Processes() []Proc { return s.processes() }

// processes — топ процессов (сортировка по CPU по убыванию; UI может пересортировать).
func (s *Sampler) processes() []Proc {
	out, _ := s.run("ps", "-eo", "pid=,user=,pcpu=,pmem=,rss=,comm=")
	var res []Proc
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 6 {
			continue
		}
		cmd := strings.Join(f[5:], " ")
		if cmd == "ps" {
			continue // не показываем сам процесс ps
		}
		pid, _ := strconv.Atoi(f[0])
		cpu, _ := strconv.ParseFloat(f[2], 64)
		mem, _ := strconv.ParseFloat(f[3], 64)
		rss, _ := strconv.ParseUint(f[4], 10, 64)
		res = append(res, Proc{PID: pid, User: f[1], CPUPct: cpu, MemPct: mem, RSSKB: rss, Command: cmd})
	}
	sort.Slice(res, func(i, j int) bool { return res[i].CPUPct > res[j].CPUPct })
	return res
}

func (s *Sampler) failedUnits(now time.Time) []string {
	// Перезапрашиваем список только если (а) ещё ни разу не читали, или
	// (б) было kill-событие от обёртки run() и прошло не менее 30 c.
	s.errMu.Lock()
	kill := s.killFlag
	s.errMu.Unlock()
	if !s.failedAt.IsZero() {
		if now.Sub(s.failedAt) < 30*time.Second || !kill {
			return s.failedCache
		}
	}
	out, _ := s.run("systemctl", "--failed", "--no-legend", "--plain")
	var failed []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		failed = append(failed, strings.Fields(line)[0])
	}
	s.failedCache = failed
	s.failedAt = now
	s.errMu.Lock()
	s.killFlag = false
	s.errMu.Unlock()
	return failed
}
