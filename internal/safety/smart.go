package safety

import (
	"os/exec"
	"strconv"
	"strings"
)

type SMARTStatus struct {
	Device      string
	Healthy     bool
	Temperature int
	Reallocated int
	Pending     int
	PowerOnHrs  int
	Errors      []string
}

func CheckSMART(device string) (*SMARTStatus, error) {
	out, _ := exec.Command("smartctl", "-A", "-H", device).CombinedOutput()
	s := &SMARTStatus{Device: device, Healthy: true}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "SMART overall-health") {
			if !strings.Contains(line, "PASSED") {
				s.Healthy = false
				s.Errors = append(s.Errors, "SMART health check FAILED")
			}
		}
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		attr := fields[1]
		raw, _ := strconv.Atoi(fields[9])
		switch attr {
		case "Temperature_Celsius", "Airflow_Temperature_Cel":
			s.Temperature = raw
		case "Reallocated_Sector_Ct":
			s.Reallocated = raw
			if raw > 0 {
				s.Errors = append(s.Errors, "reallocated sectors: "+fields[9])
			}
		case "Current_Pending_Sector":
			s.Pending = raw
			if raw > 0 {
				s.Errors = append(s.Errors, "pending sectors: "+fields[9])
			}
		case "Power_On_Hours":
			s.PowerOnHrs = raw
		}
	}
	return s, nil
}
