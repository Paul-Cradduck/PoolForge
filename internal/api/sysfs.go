package api

import (
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
)

var sysFS fs.ReadDirFS = os.DirFS("/sys/block").(fs.ReadDirFS)

func readSysBlockSize(name string) uint64 {
	data, err := os.ReadFile(fmt.Sprintf("/sys/block/%s/size", name))
	if err != nil {
		return 0
	}
	sectors, _ := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	return sectors * 512
}
