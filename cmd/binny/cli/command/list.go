package command

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/itchyny/gojq"
	"github.com/jedib0t/go-pretty/table"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/anchore/binny"
	"github.com/anchore/binny/cmd/binny/cli/option"
	"github.com/anchore/binny/internal/bus"
	"github.com/anchore/binny/tool"
	"github.com/anchore/clio"
)

type ListConfig struct {
	Config        string `json:"config" yaml:"config" mapstructure:"config"`
	option.Check  `json:"" yaml:",inline" mapstructure:",squash"`
	option.Core   `json:"" yaml:",inline" mapstructure:",squash"`
	option.List   `json:"" yaml:",inline" mapstructure:",squash"`
	option.Format `json:"" yaml:",inline" mapstructure:",squash"`
}

func List(app clio.Application) *cobra.Command {
	cfg := &ListConfig{
		Core: option.DefaultCore(),
		Format: option.Format{
			Output:           "table",
			AllowableFormats: []string{"table", "json"},
		},
	}

	return app.SetupCommand(&cobra.Command{
		Use:   "list",
		Short: "List configured and installed tool status",
		Aliases: []string{
			"ls",
		},
		Args: cobra.ArbitraryArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if cfg.Format.JQCommand != "" && cfg.Format.Output != "json" {
				return fmt.Errorf("--jq can only be used when --output format is 'json'")
			}
			cfg.IncludeFilter = args
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(*cfg)
		},
	}, cfg)
}

type toolStatus struct {
	Name             string `json:"name"`
	WantVersion      string `json:"wantVersion"`      // this is the version the user asked for
	ResolvedVersion  string `json:"resolvedVersion"`  // if the user asks for a non-specific version (e.g. "latest") then this is what that would resolve to at this point in time
	InstalledVersion string `json:"installedVersion"` // the actual version that is installed, which could vary from the user wanted or resolved values
	Constraint       string `json:"constraint"`       // the version constraint the user asked for and used during version resolution
	IsInstalled      bool   `json:"isInstalled"`      // is the tool installed at the desired version (says nothing about it being valid, only present)
	HashIsValid      bool   `json:"hashIsValid"`      // is the installed tool have the correct xxh64 hash?
	Error            error  `json:"error,omitempty"`  // if there was an error getting the status for this tool, it will be here
}

func runList(cmdCfg ListConfig) error {
	// get the current store state
	store, err := binny.NewStore(cmdCfg.Store.Root)
	if err != nil {
		return err
	}

	allStatuses := getAllStatuses(cmdCfg, store)

	// look for items in the store root that cannot be accounted for
	// TODO

	statuses := filterStatus(allStatuses, cmdCfg.List.IncludeFilter)

	if cmdCfg.Format.Output == "json" {
		return reportOnBus(renderListJSON(statuses, cmdCfg.List.Updates, cmdCfg.Format.JQCommand))
	}

	if cmdCfg.List.Updates {
		return reportOnBus(renderListUpdatesTable(statuses), nil)
	}

	return reportOnBus(renderListTable(statuses), nil)
}

func reportOnBus(value string, err error) error {
	if err != nil {
		return err
	}
	bus.Report(value)
	return nil
}

func filterStatus(status []toolStatus, includeFilter []string) []toolStatus {
	if len(includeFilter) == 0 {
		return status
	}

	filtered := make([]toolStatus, 0)
	for _, s := range status {
		for _, f := range includeFilter {
			if s.Name == f {
				filtered = append(filtered, s)
			}
		}
	}

	return filtered
}

func filterToolsWithoutUpdates(statuses []toolStatus) []toolStatus {
	var updates []toolStatus
	for _, status := range statuses {
		if status.Error != nil {
			status.InstalledVersion = ""
			status.ResolvedVersion = ""
			status.Constraint = ""
			status.HashIsValid = false
			updates = append(updates, status)
			continue
		}

		if status.WantVersion == "?" {
			continue
		}

		if status.InstalledVersion != status.ResolvedVersion {
			updates = append(updates, status)
			continue
		}

		if !status.HashIsValid {
			updates = append(updates, status)
		}
	}
	return updates
}

func getAllStatuses(cmdCfg ListConfig, store *binny.Store) []toolStatus {
	var (
		failedTools = make(map[string]error)
		allStatus   []toolStatus
	)

	names, toolOpts := selectNamesAndConfigs(cmdCfg.Core, nil)

	storedEntries := store.Entries()

	for _, opt := range toolOpts {
		status, entry, err := getStatus(store, opt)
		if err != nil {
			failedTools[opt.Name] = err
			continue
		}

		storedEntries = removeEntry(storedEntries, entry)

		if status != nil {
			allStatus = append(allStatus, *status)
		}
	}

	// what remains is tools in the store that are not configured
	// this can happen if the user configures a tool, installs it, then removes the configuration... in which case
	// the tool is still in the store but future actions will result in no action taken against it in the store.
	for _, entry := range storedEntries {
		names = append(names, entry.Name)
		installedVersion, isHashValid, err := getInstallationStatus(entry)
		if err != nil {
			failedTools[entry.Name] = err
			continue
		}
		allStatus = append(allStatus, toolStatus{
			Name:             entry.Name,
			WantVersion:      "?",
			ResolvedVersion:  "",
			Constraint:       "",
			IsInstalled:      true,
			HashIsValid:      isHashValid,
			InstalledVersion: installedVersion,
		})
	}

	// we weren't able to get status for all tools, but we should still present these
	for name, err := range failedTools {
		opt := cmdCfg.Core.Tools.GetOption(name)
		var wantVersion string
		if opt != nil {
			wantVersion = opt.Version.Want
		}
		allStatus = append(allStatus, toolStatus{
			Name:        name,
			WantVersion: wantVersion,
			Error:       err,
		})
	}

	return allStatus
}

func getStatus(store *binny.Store, opt option.Tool) (*toolStatus, *binny.StoreEntry, error) {
	t, intent, err := opt.ToTool()
	if err != nil {
		return nil, nil, err
	}

	entries := store.GetByName(t.Name())
	if len(entries) > 1 {
		return nil, nil, binny.ErrMultipleInstallations
	}

	var (
		isHashValid      bool
		installedVersion string
		isInstalled      = len(entries) == 1
		entry            *binny.StoreEntry
	)

	if isInstalled {
		entry = &entries[0]

		installedVersion, isHashValid, err = getInstallationStatus(*entry)
		if err != nil {
			return nil, nil, err
		}
	}

	resolvedVersion, err := tool.ResolveVersion(t, *intent)
	if err != nil {
		return nil, nil, err
	}

	return &toolStatus{
		Name:             opt.Name,
		WantVersion:      opt.Version.Want,
		ResolvedVersion:  resolvedVersion,
		Constraint:       opt.Version.Constraint,
		IsInstalled:      isInstalled,
		HashIsValid:      isHashValid,
		InstalledVersion: installedVersion,
	}, entry, nil
}

func getInstallationStatus(entry binny.StoreEntry) (installedVersion string, isHashValid bool, err error) {
	installedVersion = entry.InstalledVersion

	err = entry.Verify(true, false)
	if err != nil {
		var errMismatch *binny.ErrDigestMismatch
		if !errors.As(err, &errMismatch) {
			// TODO: bail if something more fundamental is wrong? or should we continue and note an error?
			return "", false, err
		}
		// TODO: should we show the mismatched hash on the UI?
	}
	isHashValid = err == nil
	err = nil
	return
}

func removeEntry(entries []binny.StoreEntry, entry *binny.StoreEntry) []binny.StoreEntry {
	if entry == nil {
		return entries
	}
	for idx, e := range entries {
		if e.Name == entry.Name && e.InstalledVersion == entry.InstalledVersion {
			return append(entries[:idx], entries[idx+1:]...)
		}
	}
	return entries
}

func renderListJSON(statuses []toolStatus, updatesOnly bool, jqCommand string) (string, error) {
	if updatesOnly {
		statuses = filterToolsWithoutUpdates(statuses)
	}

	doc := make(map[string]any)
	if statuses == nil {
		// always allocate collections
		statuses = make([]toolStatus, 0)
	}
	doc["tools"] = statuses

	buf := bytes.Buffer{}
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return "", fmt.Errorf("unable to encode JSON: %w", err)
	}

	if jqCommand != "" {
		query, err := gojq.Parse(jqCommand)
		if err != nil {
			return "", fmt.Errorf("unable to parse JQ command: %w", err)
		}

		decodeDoc := make(map[string]any)
		if err := json.Unmarshal(buf.Bytes(), &decodeDoc); err != nil {
			return "", fmt.Errorf("unable to decode JSON: %w", err)
		}

		buf.Reset()
		iter := query.Run(decodeDoc)
		for {
			v, ok := iter.Next()
			if !ok {
				break
			}
			if err, ok := v.(error); ok {
				return "", fmt.Errorf("error executing JQ command: %w", err)
			}

			if err := enc.Encode(v); err != nil {
				return "", fmt.Errorf("unable to encode JSON: %w", err)
			}
		}
	}

	result := buf.String()

	// default to raw output for simple JSON output
	if strings.HasPrefix(result, "\"") && strings.HasSuffix(result, "\"\n") {
		result = strings.ReplaceAll(result, "\"", "")
	}

	return result, nil
}

func renderListUpdatesTable(statuses []toolStatus) string {
	if len(statuses) == 0 {
		return "no tools to check"
	}

	t := table.NewWriter()
	t.SetStyle(table.StyleLight)
	t.Style().Options.DrawBorder = false
	t.Style().Options.SeparateColumns = false

	titles := []string{
		"Tool", "Update",
	}

	var header table.Row
	for _, title := range titles {
		header = append(header, title)
	}
	t.AppendHeader(header)

	var rows []table.Row
	for _, status := range statuses {
		row := getToolUpdatesRow(status)
		if row != nil {
			rows = append(rows, row)
		}
	}

	if len(rows) == 0 {
		return "all tools up to date"
	}

	sort.Slice(rows, func(i, j int) bool {
		// sort by name
		return rows[i][0].(string) < rows[j][0].(string)
	})

	for _, row := range rows {
		t.AppendRow(row)
	}

	return t.Render()
}

func getToolUpdatesRow(item toolStatus) table.Row {
	var (
		commentary string
		style      lipgloss.Style
	)

	if item.Error != nil {
		commentary = item.Error.Error()
		style = badStatus
	} else {
		if !item.IsInstalled {
			commentary = "not installed"
		} else {
			switch {
			case item.WantVersion == "?":
				commentary = ""
			case item.InstalledVersion != item.ResolvedVersion:
				commentary = fmt.Sprintf("%s → %s", summarizeGitVersion(item.InstalledVersion), summarizeGitVersion(item.ResolvedVersion))
			case !item.HashIsValid:
				commentary = ""
			}
		}
	}

	if commentary == "" {
		return nil
	}

	row := table.Row{
		item.Name,
		style.Render(commentary),
	}

	return row
}

func renderListTable(statuses []toolStatus) string {
	if len(statuses) == 0 {
		return "no tools configured or installed"
	}
	t := table.NewWriter()
	t.SetStyle(table.StyleLight)
	t.Style().Options.DrawBorder = false
	t.Style().Options.SeparateColumns = false

	// Fields:
	// Name, Wanted, Resolved, Constraint, Installed, Hash is valid, InstalledVersion

	// ok = is installed
	// ok &= resolved version == installed version
	// ok &= hash is valid

	// Column content
	// Name, Version (Resolved if not matching), [Constraint], Commentary if not valid

	var constraintNeeded bool
	for _, status := range statuses {
		if status.Constraint != "" {
			constraintNeeded = true
			break
		}
	}

	titles := []string{
		"Tool", "Desired Version", "Constraint", "",
	}

	var header table.Row
	for _, title := range titles {
		if title == "Constraint" && !constraintNeeded {
			continue
		}
		header = append(header, title)
	}
	t.AppendHeader(header)

	var rows []table.Row
	for _, status := range statuses {
		rows = append(rows, getToolStatusRow(status, constraintNeeded))
	}

	sort.Slice(rows, func(i, j int) bool {
		// sort by name
		return rows[i][0].(string) < rows[j][0].(string)
	})

	for _, row := range rows {
		t.AppendRow(row)
	}

	return t.Render()
}

func getToolStatusRow(item toolStatus, constraintNeeded bool) table.Row {
	var (
		commentary string
		severity   int
	)

	if item.Error != nil {
		commentary = item.Error.Error()
		severity = 2
	} else {
		if !item.IsInstalled {
			commentary = "not installed"
			severity = 1
		} else {
			switch {
			case item.WantVersion == "?":
				commentary = "tool is not configured"
				severity = 2
			case item.InstalledVersion != item.ResolvedVersion:
				commentary = fmt.Sprintf("installed version (%s) does not match resolved version (%s)", summarizeGitVersion(item.InstalledVersion), summarizeGitVersion(item.ResolvedVersion))
				severity = 1
			case !item.HashIsValid:
				commentary = "hash is invalid"
				severity = 2
			}
		}
	}

	version := item.WantVersion

	if item.WantVersion != item.ResolvedVersion && item.ResolvedVersion != "" {
		version += fmt.Sprintf(" (%s)", summarizeGitVersion(item.ResolvedVersion))
	}

	style := toolStatusStyle(severity)

	row := table.Row{
		item.Name,
		style.Render(version),
	}

	if constraintNeeded {
		row = append(row, item.Constraint)
	}

	row = append(row, style.Render(commentary))

	return row
}

func summarizeGitVersion(v string) string {
	// if it looks like a git hash, then summarize it
	if len(v) == 40 && onlyAlphaNumeric(v) {
		return v[:7]
	}
	return v
}

func onlyAlphaNumeric(v string) bool {
	for _, c := range v {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

var (
	goodStatus      = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))  // 10 = high intensity green (ANSI 16 bit color code)
	badStatus       = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // 214 = orange1 (ANSI 16 bit color code)
	reallyBadStatus = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))   // 9 = high intensity red (ANSI 16 bit color code)
)

func toolStatusStyle(severity int) lipgloss.Style {
	switch severity {
	case 0:
		return goodStatus
	case 1:
		return badStatus
	}

	return reallyBadStatus
}
