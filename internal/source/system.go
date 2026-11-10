package source

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
)

// ServerReader reads the figures of the machine the dashboard runs on.
type ServerReader interface {
	ReadServer() model.ServerDetails
}

// SettingsReader reads the node's main settings (story #13). running is
// what the node reports, nil while it can't be reached.
type SettingsReader interface {
	ReadSettings(running *model.RunningSettings) *model.Settings
}

// System reads server figures from Linux's /proc and /sys (story #36).
// Anything it can't read is reported in Missing, never as an error.
type System struct {
	// Root is "/" normally, or a directory with fixture files in tests.
	Root string
	// DataDir is the Bitcoin data directory, for the disk figures.
	DataDir string
	disk    func(path string) (free, total uint64, err error)
	goos    string
}

// NewSystem returns a System for the real machine.
func NewSystem(dataDir string) *System {
	return &System{Root: "/", DataDir: dataDir, disk: diskUsage, goos: runtime.GOOS}
}

// cpuSensors are thermal zone types that measure the CPU, best first.
var cpuSensors = []string{"x86_pkg_temp", "cpu-thermal", "cpu_thermal", "soc-thermal", "soc_thermal", "coretemp", "k10temp", "cpu"}

// ReadServer implements ServerReader.
func (s *System) ReadServer() model.ServerDetails {
	d := model.ServerDetails{Missing: map[string]string{}}
	notLinux := s.goos != "linux"
	missing := func(key string, err error, why string) {
		switch {
		case notLinux:
			d.Missing[key] = "only available on Linux"
		case errors.Is(err, fs.ErrNotExist) || err == nil:
			d.Missing[key] = why
		default:
			d.Missing[key] = "can’t be read: " + err.Error()
		}
	}

	if cpu, err := s.readCPU(); err == nil {
		d.CPU = cpu
	} else {
		missing("cpu", err, "not available on this server")
	}
	if used, total, err := s.readMemory(); err == nil {
		d.MemUsedBytes, d.MemTotalBytes = used, total
	} else {
		missing("memory", err, "not available on this server")
	}
	if up, err := s.readUptime(); err == nil {
		d.UptimeSec = up
	} else {
		missing("uptime", err, "not available on this server")
	}
	if t, err := s.readTemperature(); err == nil {
		d.CPUTempC = &t
	} else if s.inContainer() {
		missing("temperature", err, "not available in Docker")
	} else {
		missing("temperature", err, "this server doesn’t expose a CPU sensor (common in virtual machines)")
	}

	if s.DataDir == "" || s.disk == nil {
		d.Missing["disk"] = "the Bitcoin data directory is unknown"
	} else if free, total, err := s.disk(s.DataDir); err == nil {
		d.DiskFreeBytes, d.DiskTotalBytes = free, total
		d.DiskUsedBytes = total - min(free, total)
	} else {
		d.Missing["disk"] = "can’t read the disk of " + s.DataDir
	}
	return d
}

func (s *System) path(p string) string { return filepath.Join(s.Root, p) }

// readCPU reads the first line of /proc/stat:
// "cpu  user nice system idle iowait irq softirq steal guest guest_nice".
func (s *System) readCPU() (model.CPUSample, error) {
	f, err := os.Open(s.path("proc/stat"))
	if err != nil {
		return model.CPUSample{}, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return model.CPUSample{}, errors.New("empty /proc/stat")
	}
	fields := strings.Fields(sc.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return model.CPUSample{}, errors.New("unexpected /proc/stat")
	}
	var c model.CPUSample
	// guest and guest_nice are already counted in user and nice.
	for i, v := range fields[1:min(len(fields), 9)] {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return model.CPUSample{}, errors.New("unexpected /proc/stat")
		}
		c.Total += n
		if i == 3 || i == 4 { // idle, iowait
			c.Idle += n
		}
	}
	return c, nil
}

// readMemory reads MemTotal and MemAvailable from /proc/meminfo.
func (s *System) readMemory() (used, total uint64, err error) {
	b, err := os.ReadFile(s.path("proc/meminfo"))
	if err != nil {
		return 0, 0, err
	}
	kb := map[string]uint64{}
	for _, line := range strings.Split(string(b), "\n") {
		key, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) > 0 {
			if n, err := strconv.ParseUint(f[0], 10, 64); err == nil {
				kb[key] = n
			}
		}
	}
	total, avail := kb["MemTotal"], kb["MemAvailable"]
	if total == 0 || avail > total {
		return 0, 0, errors.New("unexpected /proc/meminfo")
	}
	return (total - avail) * 1024, total * 1024, nil
}

// readUptime reads the seconds since boot from /proc/uptime.
func (s *System) readUptime() (int64, error) {
	b, err := os.ReadFile(s.path("proc/uptime"))
	if err != nil {
		return 0, err
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return 0, errors.New("unexpected /proc/uptime")
	}
	up, err := strconv.ParseFloat(f[0], 64)
	if err != nil {
		return 0, errors.New("unexpected /proc/uptime")
	}
	return int64(up), nil
}

// readTemperature returns the temperature of the best CPU sensor among the
// thermal zones, in °C. Other sensors (e.g. the motherboard's) don't count.
func (s *System) readTemperature() (float64, error) {
	zones, _ := filepath.Glob(s.path("sys/class/thermal/thermal_zone*"))
	best, bestRank := -1.0, len(cpuSensors)
	for _, z := range zones {
		t, err := os.ReadFile(filepath.Join(z, "type"))
		if err != nil {
			continue
		}
		typ := strings.TrimSpace(string(t))
		rank := len(cpuSensors)
		for i, c := range cpuSensors {
			if typ == c {
				rank = i
				break
			}
		}
		if rank >= bestRank {
			continue
		}
		v, err := os.ReadFile(filepath.Join(z, "temp"))
		if err != nil {
			continue
		}
		milli, err := strconv.ParseInt(strings.TrimSpace(string(v)), 10, 64)
		if err != nil || milli <= 0 {
			continue
		}
		best, bestRank = float64(milli)/1000, rank
	}
	if best < 0 {
		return 0, fs.ErrNotExist
	}
	return best, nil
}

// inContainer reports whether the dashboard runs in Docker or Podman.
func (s *System) inContainer() bool {
	for _, p := range []string{".dockerenv", "run/.containerenv"} {
		if _, err := os.Stat(s.path(p)); err == nil {
			return true
		}
	}
	return false
}
