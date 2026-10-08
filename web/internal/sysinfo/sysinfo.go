package sysinfo

import (
	"bufio"
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
	Throttled string `json:"throttled"`
	Volts     string `json:"volts"`
}

type Info struct {
	Host   Host     `json:"host"`
	CPU    CPU      `json:"cpu"`
	Memory Memory   `json:"memory"`
	Disks  []Disk   `json:"disks"`
	Net    Network  `json:"network"`
	Power  Power    `json:"power"`
	Failed []string `json:"failed_units"`
	TS     int64    `json:"ts"`
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
	info.Failed = s.failedUnits(now)
	if info.Failed == nil {
		info.Failed = []string{}
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

func (s *Sampler) power(now time.Time) Power {
	if now.Sub(s.powerCacheAt) < 5*time.Second {
		return s.powerCache
	}
	var p Power
	p.Throttled = cmdOut("vcgencmd", "get_throttled")
	p.Volts = cmdOut("vcgencmd", "measure_volts")
	s.powerCache = p
	s.powerCacheAt = now
	return p
}

func (s *Sampler) failedUnits(now time.Time) []string {
	if now.Sub(s.failedAt) < 10*time.Second {
		return s.failedCache
	}
	out := cmdOutRaw("systemctl", "--failed", "--no-legend", "--plain")
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
	return failed
}

func cmdOut(name string, args ...string) string {
	return strings.TrimSpace(cmdOutRaw(name, args...))
}

func cmdOutRaw(name string, args ...string) string {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
