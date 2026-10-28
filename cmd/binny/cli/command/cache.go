package command

import (
	"fmt"

	"github.com/jedib0t/go-pretty/table"
	"github.com/spf13/cobra"

	"github.com/anchore/binny"
	"github.com/anchore/binny/cmd/binny/cli/option"
	"github.com/anchore/clio"
)

// Cache is the parent of the commands that manage the cache. They read cache.root whether or not the cache is
// enabled, so entries left from when it was enabled can still be listed and removed.
func Cache(app clio.Application) *cobra.Command {
	cmd := app.SetupCommand(&cobra.Command{
		Use:   "cache",
		Short: "Manage the tool cache shared between stores",
	})

	cmd.AddCommand(
		CacheList(app),
	)

	return cmd
}

type CacheListConfig struct {
	Cache         option.Cache `json:"cache" yaml:"cache" mapstructure:"cache"`
	option.Format `json:"" yaml:",inline" mapstructure:",squash"`
}

func CacheList(app clio.Application) *cobra.Command {
	cfg := &CacheListConfig{
		Cache: option.DefaultCache(),
		Format: option.Format{
			Output:           formatTable,
			AllowableFormats: []string{formatTable, formatJSON},
		},
	}

	return app.SetupCommand(&cobra.Command{
		Use:   "list",
		Short: "List the tools in the cache",
		Aliases: []string{
			"ls",
		},
		Args: cobra.NoArgs,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if cfg.JQCommand != "" && cfg.Output != formatJSON {
				return fmt.Errorf("--jq can only be used when --output format is 'json'")
			}
			return nil
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			return runCacheList(*cfg)
		},
	}, cfg)
}

type cacheEntryInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Fingerprint string `json:"fingerprint"`
	Size        int64  `json:"size"` // in bytes
	Path        string `json:"path"`
}

func runCacheList(cfg CacheListConfig) error {
	cache, err := binny.NewCache(cfg.Cache.Root)
	if err != nil {
		return err
	}

	entries, err := cache.List()
	if err != nil {
		return err
	}

	infos := make([]cacheEntryInfo, 0, len(entries))
	for _, e := range entries {
		size, err := e.Size()
		if err != nil {
			return err
		}
		infos = append(infos, cacheEntryInfo{
			Name:        e.Name,
			Version:     e.Version,
			Fingerprint: e.Fingerprint,
			Size:        size,
			Path:        e.Dir,
		})
	}

	if cfg.Output == formatJSON {
		return reportOnBus(renderCacheListJSON(infos, cfg.JQCommand))
	}
	return reportOnBus(renderCacheListTable(infos), nil)
}

func renderCacheListJSON(entries []cacheEntryInfo, jqCommand string) (string, error) {
	if entries == nil {
		// always allocate collections
		entries = make([]cacheEntryInfo, 0)
	}
	return renderJSON(map[string]any{"entries": entries}, jqCommand)
}

func renderCacheListTable(entries []cacheEntryInfo) string {
	if len(entries) == 0 {
		return "no cached tools"
	}

	t := table.NewWriter()
	t.SetStyle(table.StyleLight)
	t.Style().Options.DrawBorder = false
	t.Style().Options.SeparateColumns = false

	t.AppendHeader(table.Row{"Name", "Version", "Fingerprint", "Size", "Path"})
	for _, e := range entries {
		t.AppendRow(table.Row{e.Name, e.Version, e.Fingerprint, formatSize(e.Size), e.Path})
	}

	return t.Render()
}

// formatSize renders a byte count with a binary unit, e.g. 1.5 MiB.
func formatSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(size)/float64(div), "KMGTPE"[exp])
}
