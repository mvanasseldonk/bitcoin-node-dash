package source

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture writes files under a temporary root, like /proc and /sys.
func fixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func testSystem(root string) *System {
	return &System{
		Root: root, DataDir: "/data", goos: "linux",
		disk: func(string) (uint64, uint64, error) { return 812e9, 2000e9, nil },
	}
}

var linuxFiles = map[string]string{
	"proc/stat":                            "cpu  4705 356 584 3699176 23060 0 277 0 0 0\ncpu0 1393 280 284 925436 5765 0 147 0 0 0\nintr 1\n",
	"proc/meminfo":                         "MemTotal:       16314736 kB\nMemFree:         2034628 kB\nMemAvailable:   10584536 kB\nBuffers:          301132 kB\n",
	"proc/uptime":                          "3543093.48 13995540.32\n",
	"sys/class/thermal/thermal_zone0/type": "acpitz\n",
	"sys/class/thermal/thermal_zone0/temp": "27800\n",
	"sys/class/thermal/thermal_zone1/type": "x86_pkg_temp\n",
	"sys/class/thermal/thermal_zone1/temp": "46000\n",
}

func TestReadServer(t *testing.T) {
	d := testSystem(fixture(t, linuxFiles)).ReadServer()
	if len(d.Missing) != 0 {
		t.Fatalf("missing: %v", d.Missing)
	}
	// user+nice+system+idle+iowait+irq+softirq+steal; idle = idle+iowait
	if d.CPU.Total != 4705+356+584+3699176+23060+277 || d.CPU.Idle != 3699176+23060 {
		t.Errorf("cpu = %+v", d.CPU)
	}
	if d.MemTotalBytes != 16314736*1024 || d.MemUsedBytes != (16314736-10584536)*1024 {
		t.Errorf("memory = %d / %d", d.MemUsedBytes, d.MemTotalBytes)
	}
	if d.UptimeSec != 3543093 {
		t.Errorf("uptime = %d", d.UptimeSec)
	}
	// The CPU package sensor, not the motherboard's (acpitz).
	if d.CPUTempC == nil || *d.CPUTempC != 46 {
		t.Errorf("temperature = %v, want 46", d.CPUTempC)
	}
	if d.DiskTotalBytes != 2000e9 || d.DiskFreeBytes != 812e9 || d.DiskUsedBytes != 1188e9 {
		t.Errorf("disk = %d / %d / %d", d.DiskFreeBytes, d.DiskUsedBytes, d.DiskTotalBytes)
	}
}

func TestReadServerVirtualMachine(t *testing.T) {
	files := map[string]string{}
	for k, v := range linuxFiles {
		if !strings.HasPrefix(k, "sys/") {
			files[k] = v
		}
	}
	files["sys/class/thermal/thermal_zone0/type"] = "acpitz\n" // not a CPU sensor
	files["sys/class/thermal/thermal_zone0/temp"] = "16800\n"
	d := testSystem(fixture(t, files)).ReadServer()
	if d.CPUTempC != nil || !strings.Contains(d.Missing["temperature"], "virtual machines") {
		t.Fatalf("temperature = %v, missing = %q", d.CPUTempC, d.Missing["temperature"])
	}
	if len(d.Missing) != 1 {
		t.Fatalf("only the temperature should be missing: %v", d.Missing)
	}
}

func TestReadServerDocker(t *testing.T) {
	files := map[string]string{".dockerenv": ""}
	for k, v := range linuxFiles {
		if !strings.HasPrefix(k, "sys/") {
			files[k] = v
		}
	}
	d := testSystem(fixture(t, files)).ReadServer()
	if d.Missing["temperature"] != "not available in Docker" {
		t.Fatalf("missing = %v", d.Missing)
	}
}

func TestReadServerNothingThere(t *testing.T) {
	s := testSystem(t.TempDir())
	s.disk = func(string) (uint64, uint64, error) { return 0, 0, errors.New("no such file") }
	d := s.ReadServer()
	for _, key := range []string{"cpu", "memory", "uptime", "temperature", "disk"} {
		if d.Missing[key] == "" {
			t.Errorf("%s: no reason given", key)
		}
	}
	if d.MemTotalBytes != 0 || d.UptimeSec != 0 || d.CPUTempC != nil {
		t.Errorf("want unknown values, got %+v", d)
	}
}

func TestReadServerNotLinux(t *testing.T) {
	s := testSystem(t.TempDir())
	s.goos = "darwin"
	if got := s.ReadServer().Missing["memory"]; got != "only available on Linux" {
		t.Fatalf("missing memory = %q", got)
	}
}

func TestReadServerGarbage(t *testing.T) {
	d := testSystem(fixture(t, map[string]string{
		"proc/stat":    "garbage\n",
		"proc/meminfo": "MemTotal: lots\n",
		"proc/uptime":  "\n",
	})).ReadServer()
	if !strings.HasPrefix(d.Missing["cpu"], "can’t be read") || !strings.HasPrefix(d.Missing["memory"], "can’t be read") {
		t.Fatalf("missing = %v", d.Missing)
	}
}
