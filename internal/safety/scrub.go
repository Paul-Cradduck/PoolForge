package safety

import (
	"fmt"
	"os/exec"
	"time"
)

type ScrubScheduler struct {
	interval time.Duration
	stop     chan struct{}
	onError  func(device string, err error)
}

func NewScrubScheduler(interval time.Duration, onError func(string, error)) *ScrubScheduler {
	return &ScrubScheduler{interval: interval, stop: make(chan struct{}), onError: onError}
}

func (s *ScrubScheduler) Start(arrays []string) {
	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-ticker.C:
				for _, dev := range arrays {
					if err := StartScrub(dev); err != nil && s.onError != nil {
						s.onError(dev, err)
					}
				}
			}
		}
	}()
}

func (s *ScrubScheduler) Stop() { close(s.stop) }

func StartScrub(device string) error {
	// Trigger a check via sysfs
	sysPath := fmt.Sprintf("/sys/block/%s/md/sync_action", device[len("/dev/"):])
	out, err := exec.Command("bash", "-c", fmt.Sprintf("echo check > %s", sysPath)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("scrub %s: %w\n%s", device, err, out)
	}
	return nil
}
