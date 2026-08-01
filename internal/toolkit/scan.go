package toolkit

import (
	"os/exec"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// ScanConfig passes necessary context for scanning the host toolkit.
type ScanConfig struct {
	Settings *config.Settings
	WebReady bool
	GOOS     string
}

// Report holds the result of a toolkit scan.
type Report struct {
	Groups []GroupResult
}

// GroupResult is the scan result for a single group.
type GroupResult struct {
	Name  string
	Items []ItemResult
}

// ItemResult is the scan result for a single item.
type ItemResult struct {
	Name        string
	Installed   bool
	InstallHint string
}

// Scan checks the host for the items defined in the Catalog.
func Scan(cfg ScanConfig) Report {
	var rep Report
	for _, g := range Catalog() {
		if g.SkipIf != nil && g.SkipIf(cfg) {
			continue
		}

		var gr GroupResult
		gr.Name = g.Name
		for _, it := range g.Items {
			installed := false
			if it.DetectFunc != nil {
				installed = it.DetectFunc(cfg)
			} else {
				for _, cmd := range it.Commands {
					if _, err := lookPath(cmd); err == nil {
						installed = true
						break
					}
				}
			}

			gr.Items = append(gr.Items, ItemResult{
				Name:        it.Name,
				Installed:   installed,
				InstallHint: it.InstallHint,
			})
		}
		rep.Groups = append(rep.Groups, gr)
	}
	return rep
}

var lookPath = exec.LookPath
