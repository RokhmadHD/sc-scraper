package main

import (
	"bufio"
	"context"
	"database/sql"
	"flag"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	_ "github.com/lib/pq"

	"scrape-smart-contract/internal/chains"
	"scrape-smart-contract/internal/eth"
	"scrape-smart-contract/internal/rpc"
	"scrape-smart-contract/internal/scraper"
	"scrape-smart-contract/internal/storage"
)

type config struct {
	postgresURL     string
	postgresTable   string
	envPath         string
	envLoaded       bool
	envHasURL       bool
	chain           string
	startBlock      uint64
	endBlock        uint64
	last            uint64
	concurrency     int
	minBalance      string
	continueOnError bool
	timeout         time.Duration
	retries         int
	resetDB         bool
}

type model struct {
	cfg           config
	selected      int
	editing       bool
	editValue     string
	choosingChain bool
	chainCursor   int
	width         int
	height        int
	stats         dbStats
	recent        []recentContract
	topRPCs       []rpcStat
	hasRuntimeRPC bool
	logs          []string
	running       bool
	startedAt     time.Time
	err           string
	logCh         chan string
	rpcStatsCh    chan []rpcStat
	doneCh        chan scrapeDone
}

type dbStats struct {
	Connected      bool
	Total          int64
	Chains         int64
	LatestBlock    uint64
	LastUpdated    time.Time
	LastContract   string
	LastBlock      uint64
	LastNetwork    string
	LastInsertedAt string
	Error          string
}

type recentContract struct {
	Network string
	Block   uint64
	Address string
}

type rpcStat struct {
	URL   string
	Calls int64
}

type statsMsg dbStats
type recentMsg []recentContract
type rpcStatsMsg []rpcStat
type rpcStatsPollMsg struct{}
type logMsg string

type scrapeDone struct {
	Count int
	Err   error
}

type doneMsg scrapeDone
type tickMsg time.Time

var settingFields = []string{"chain", "start-block", "end-block", "last", "concurrency", "min-balance", "retries", "timeout"}
var chainChoices = []string{"eth", "base", "arb1", "op", "matic", "bsc", "avax", "sep"}
var minBalanceChoices = []string{"0", "0.001", "0.01", "0.1", "1"}

var (
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(0, 1)
	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("86")).
			Bold(true)
	mutedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))
	errStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("203"))
	okStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("42"))
)

func main() {
	envInfo := loadDotEnv(".env")
	var cfg config
	flag.StringVar(&cfg.postgresURL, "postgres-url", env("POSTGRES_URL", "postgres://sc_scraper:sc_scraper_password@localhost:5432/sc_scraper?sslmode=disable"), "PostgreSQL connection URL")
	flag.StringVar(&cfg.postgresTable, "postgres-table", "contract_creations", "PostgreSQL table")
	flag.StringVar(&cfg.chain, "chain", "eth", "chain name")
	flag.Uint64Var(&cfg.startBlock, "start-block", 0, "first block to scrape; paired with --end-block")
	flag.Uint64Var(&cfg.endBlock, "end-block", 0, "last block to scrape; paired with --start-block")
	flag.Uint64Var(&cfg.last, "last", 100, "scrape the last N blocks when pressing s")
	flag.IntVar(&cfg.concurrency, "concurrency", 4, "scraper concurrency")
	flag.StringVar(&cfg.minBalance, "min-balance", "0", "minimum native balance")
	flag.BoolVar(&cfg.continueOnError, "continue-on-error", true, "continue after a block-level RPC error")
	flag.DurationVar(&cfg.timeout, "timeout", 20*time.Second, "RPC timeout")
	flag.IntVar(&cfg.retries, "retries", 2, "RPC retries")
	flag.BoolVar(&cfg.resetDB, "reset-db", false, "drop and recreate the PostgreSQL table before scraping")
	flag.Parse()
	cfg.envPath = envInfo.Path
	cfg.envLoaded = envInfo.Loaded
	cfg.envHasURL = envInfo.HasPostgresURL || strings.TrimSpace(os.Getenv("POSTGRES_URL")) != ""
	table, err := normalizeTableName(cfg.postgresTable)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	cfg.postgresTable = table

	m := model{
		cfg:        cfg,
		logCh:      make(chan string, 200),
		rpcStatsCh: make(chan []rpcStat, 20),
		doneCh:     make(chan scrapeDone, 1),
	}
	m.logs = append([]string{"ready"}, m.configLogLines()...)
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(fetchStats(m.cfg), fetchRecent(m.cfg), fetchTopRPCs(m.cfg), tick(), drainLogs(m.logCh), drainRPCStats(m.rpcStatsCh), waitDone(m.doneCh))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		if m.choosingChain {
			return m.updateChainChoice(msg)
		}
		if m.editing {
			return m.updateEdit(msg)
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			m.addLog("refreshing database stats")
			cmds := []tea.Cmd{fetchStats(m.cfg), fetchRecent(m.cfg)}
			if !m.running && !m.hasRuntimeRPC {
				cmds = append(cmds, fetchTopRPCs(m.cfg))
			}
			return m, tea.Batch(cmds...)
		case "tab", "down":
			if !m.running {
				m.selected = (m.selected + 1) % len(settingFields)
			}
		case "shift+tab", "up":
			if !m.running {
				m.selected = (m.selected + len(settingFields) - 1) % len(settingFields)
			}
		case "right", "l", "+":
			if !m.running {
				m.adjustSelected(1)
			}
		case "left", "h", "-":
			if !m.running {
				m.adjustSelected(-1)
			}
		case "enter":
			if !m.running {
				m.startEdit()
			}
		case "s":
			if m.running {
				m.addLog("scraper already running")
				return m, nil
			}
			m.running = true
			m.startedAt = time.Now()
			m.err = ""
			m.topRPCs = nil
			m.hasRuntimeRPC = false
			m.addLog("starting scraper")
			go runScraper(m.cfg, m.logCh, m.rpcStatsCh, m.doneCh)
		}
	case statsMsg:
		m.stats = dbStats(msg)
		if m.stats.Error != "" {
			m.err = m.stats.Error
		}
	case recentMsg:
		m.recent = []recentContract(msg)
	case rpcStatsMsg:
		if len(msg) > 0 {
			m.topRPCs = []rpcStat(msg)
			m.hasRuntimeRPC = true
		}
		return m, drainRPCStats(m.rpcStatsCh)
	case rpcStatsPollMsg:
		return m, drainRPCStats(m.rpcStatsCh)
	case logMsg:
		if msg != "" {
			m.addLog(string(msg))
		}
		return m, drainLogs(m.logCh)
	case doneMsg:
		m.running = false
		if msg.Err != nil {
			m.err = msg.Err.Error()
			m.addLog("scraper failed: " + msg.Err.Error())
		} else {
			m.addLog(fmt.Sprintf("scraper finished: saved %d contracts", msg.Count))
		}
		cmds := []tea.Cmd{fetchStats(m.cfg), fetchRecent(m.cfg), waitDone(m.doneCh)}
		if !m.hasRuntimeRPC {
			cmds = append(cmds, fetchTopRPCs(m.cfg))
		}
		return m, tea.Batch(cmds...)
	case tickMsg:
		cmds := []tea.Cmd{fetchStats(m.cfg), fetchRecent(m.cfg), tick()}
		if !m.running && !m.hasRuntimeRPC {
			cmds = append(cmds, fetchTopRPCs(m.cfg))
		}
		return m, tea.Batch(cmds...)
	}
	return m, nil
}

func (m model) View() string {
	if m.width == 0 {
		return "loading..."
	}
	if m.width < 96 {
		return m.compactView()
	}
	return m.wideView()
}

func (m model) wideView() string {
	gap := 1
	outerWidth := clamp(m.width-4, 40, m.width)
	headerContentHeight := 2
	topContentHeight := clamp(m.height/5, 6, 9)
	controlsContentHeight := 7
	usedFixed := headerContentHeight + topContentHeight + 14
	remaining := max(16, m.height-usedFixed)
	midContentHeight := clamp(remaining*2/3, 12, 22)
	logContentHeight := max(8, remaining-midContentHeight)
	topWidths := splitWidths(outerWidth, gap, 2)
	midWidths := splitWidths(outerWidth, gap, 2)
	topRPCHeight := max(6, midContentHeight-controlsContentHeight-1)
	header := panelStyle.Width(outerWidth).Height(headerContentHeight).Render(m.headerPanel(outerWidth))
	db := panelStyle.Width(topWidths[0]).Height(topContentHeight).Render(m.dbPanel())
	action := panelStyle.Width(topWidths[1]).Height(topContentHeight).Render(m.actionPanel())
	recent := panelStyle.Width(midWidths[0]).Height(midContentHeight).Render(m.recentPanel())
	topRPC := panelStyle.Width(midWidths[1]).Height(topRPCHeight).Render(m.topRPCPanel())
	help := panelStyle.Width(midWidths[1]).Height(controlsContentHeight).Render(m.helpPanel())
	sidePanel := lipgloss.JoinVertical(lipgloss.Left, topRPC, help)
	logs := panelStyle.Width(outerWidth).Height(logContentHeight).Render(m.logPanel(logContentHeight - 2))

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		lipgloss.JoinHorizontal(lipgloss.Top, db, strings.Repeat(" ", gap), action),
		lipgloss.JoinHorizontal(lipgloss.Top, recent, strings.Repeat(" ", gap), sidePanel),
		logs,
	)
}

func (m model) compactView() string {
	outerWidth := clamp(m.width-2, 36, m.width)
	headerContentHeight := 2
	panelHeight := 7
	logHeight := max(8, m.height-headerContentHeight-panelHeight*5-14)
	header := panelStyle.Width(outerWidth).Height(headerContentHeight).Render(m.headerPanel(outerWidth))
	db := panelStyle.Width(outerWidth).Height(panelHeight).Render(m.dbPanel())
	action := panelStyle.Width(outerWidth).Height(max(9, panelHeight)).Render(m.actionPanel())
	recent := panelStyle.Width(outerWidth).Height(panelHeight).Render(m.recentPanel())
	topRPC := panelStyle.Width(outerWidth).Height(panelHeight).Render(m.topRPCPanel())
	help := panelStyle.Width(outerWidth).Height(panelHeight).Render(m.helpPanel())
	logs := panelStyle.Width(outerWidth).Height(logHeight).Render(m.logPanel(logHeight - 2))
	return lipgloss.JoinVertical(lipgloss.Left, header, db, action, recent, topRPC, help, logs)
}

func (m model) headerPanel(width int) string {
	columns := splitWidths(width, 1, 2)
	title := lipgloss.NewStyle().Width(columns[0]).Render(strings.Join([]string{
		titleStyle.Render("Scrape Smart Contract TUI"),
		mutedStyle.Render("PostgreSQL monitor and scraper controller"),
	}, "\n"))
	detail := lipgloss.NewStyle().Width(columns[1]).Render(strings.Join([]string{
		"creator: RokhmadHD",
		fmt.Sprintf("chain: %s  table: %s", m.cfg.chain, m.cfg.postgresTable),
	}, "\n"))
	return lipgloss.JoinHorizontal(lipgloss.Top, title, detail)
}

func (m model) dbPanel() string {
	status := errStyle.Render("offline")
	if m.stats.Connected {
		status = okStyle.Render("online")
	}
	lines := []string{
		titleStyle.Render("Database Monitor"),
		"status: " + status,
		fmt.Sprintf("table: %s", m.cfg.postgresTable),
		fmt.Sprintf("contracts: %d", m.stats.Total),
		fmt.Sprintf("chains: %d", m.stats.Chains),
		fmt.Sprintf("latest block: %d", m.stats.LatestBlock),
	}
	if m.stats.LastContract != "" {
		lines = append(lines, fmt.Sprintf("last: %s %d", m.stats.LastNetwork, m.stats.LastBlock))
		lines = append(lines, trimMiddle(m.stats.LastContract, 34))
	}
	if m.stats.Error != "" {
		lines = append(lines, errStyle.Render(m.stats.Error))
	}
	return strings.Join(lines, "\n")
}

func (m model) actionPanel() string {
	state := "idle"
	if m.running {
		state = okStyle.Render("running")
	}
	lines := []string{
		titleStyle.Render("Scraper Action"),
		"state: " + state,
		m.settingLine(0, "chain", m.cfg.chain),
		m.settingLine(1, "start block", zeroDash(m.cfg.startBlock)),
		m.settingLine(2, "end block", zeroDash(m.cfg.endBlock)),
		m.settingLine(3, "last blocks", fmt.Sprint(m.cfg.last)),
		m.settingLine(4, "concurrency", fmt.Sprint(m.cfg.concurrency)),
		m.settingLine(5, "min balance", m.cfg.minBalance),
		m.settingLine(6, "retries", fmt.Sprint(m.cfg.retries)),
		m.settingLine(7, "timeout", m.cfg.timeout.String()),
	}
	if m.running {
		lines = append(lines, fmt.Sprintf("elapsed: %s", time.Since(m.startedAt).Round(time.Second)))
	}
	if m.err != "" {
		lines = append(lines, errStyle.Render(trimMiddle(m.err, 60)))
	}
	return strings.Join(lines, "\n")
}

func (m model) recentPanel() string {
	lines := []string{titleStyle.Render("Recent Contracts")}
	if len(m.recent) == 0 {
		lines = append(lines, mutedStyle.Render("no rows yet"))
		return strings.Join(lines, "\n")
	}
	for _, item := range m.recent {
		lines = append(lines, fmt.Sprintf("%s #%d %s", item.Network, item.Block, trimMiddle(item.Address, 28)))
	}
	return strings.Join(lines, "\n")
}

func (m model) topRPCPanel() string {
	lines := []string{titleStyle.Render("Top RPC")}
	if len(m.topRPCs) == 0 {
		lines = append(lines, mutedStyle.Render("no rpc calls yet"))
		return strings.Join(lines, "\n")
	}
	for _, item := range m.topRPCs {
		lines = append(lines, fmt.Sprintf("%dx %s", item.Calls, trimMiddle(item.URL, 44)))
	}
	return strings.Join(lines, "\n")
}

func (m model) helpPanel() string {
	return strings.Join([]string{
		titleStyle.Render("Controls"),
		"s  start scraper",
		"r  refresh database",
		"tab/up/down select setting",
		"left/right adjust setting",
		"enter edit/select",
		"chain: up/down then enter",
		"esc cancel edit",
		"q  quit",
	}, "\n")
}

func (m model) configLogLines() []string {
	return []string{
		"config " + plainCheck(m.cfg.envLoaded, ".env loaded"),
		"config " + plainCheck(m.cfg.envHasURL, "POSTGRES_URL set"),
		"config " + plainCheck(isPostgresURL(m.cfg.postgresURL), "postgres URL valid"),
		"config " + plainCheck(isValidTableName(m.cfg.postgresTable), "table name valid"),
		"config database online will be checked by monitor",
	}
}

func plainCheck(ok bool, label string) string {
	if ok {
		return "[x] " + label
	}
	return "[ ] " + label
}

func (m model) checkLine(ok bool, label string) string {
	if ok {
		return okStyle.Render("[x] ") + label
	}
	return errStyle.Render("[ ] ") + label
}

func (m model) settingLine(index int, label string, value string) string {
	prefix := "  "
	if m.selected == index && !m.running {
		prefix = "> "
	}
	if m.choosingChain && index == 0 {
		value = "[" + chainChoices[m.chainCursor] + "]"
	}
	if m.editing && m.selected == index {
		value = "[" + m.editValue + "]"
	}
	return fmt.Sprintf("%s%s: %s", prefix, label, value)
}

func (m *model) adjustSelected(delta int) {
	switch settingFields[m.selected] {
	case "chain":
		m.cfg.chain = cycleString(chainChoices, m.cfg.chain, delta)
	case "start-block":
		m.cfg.startBlock = adjustUint64(m.cfg.startBlock, delta, 1000)
	case "end-block":
		m.cfg.endBlock = adjustUint64(m.cfg.endBlock, delta, 1000)
	case "last":
		current := int64(m.cfg.last)
		step := int64(100)
		if current < step && delta < 0 {
			m.cfg.last = 1
			return
		}
		next := current + int64(delta)*step
		if next < 1 {
			next = 1
		}
		m.cfg.last = uint64(next)
	case "concurrency":
		m.cfg.concurrency += delta
		if m.cfg.concurrency < 1 {
			m.cfg.concurrency = 1
		}
		if m.cfg.concurrency > 64 {
			m.cfg.concurrency = 64
		}
	case "min-balance":
		m.cfg.minBalance = cycleString(minBalanceChoices, m.cfg.minBalance, delta)
	case "retries":
		m.cfg.retries += delta
		if m.cfg.retries < 0 {
			m.cfg.retries = 0
		}
		if m.cfg.retries > 20 {
			m.cfg.retries = 20
		}
	case "timeout":
		m.cfg.timeout += time.Duration(delta) * 5 * time.Second
		if m.cfg.timeout < 5*time.Second {
			m.cfg.timeout = 5 * time.Second
		}
		if m.cfg.timeout > 5*time.Minute {
			m.cfg.timeout = 5 * time.Minute
		}
	}
	m.addLog("setting " + settingFields[m.selected] + " updated")
}

func (m *model) startEdit() {
	if settingFields[m.selected] == "chain" {
		m.choosingChain = true
		m.chainCursor = chainChoiceIndex(m.cfg.chain)
		return
	}
	m.editing = true
	m.editValue = m.selectedValue()
	if m.editValue == "-" {
		m.editValue = ""
	}
}

func (m model) updateChainChoice(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.choosingChain = false
	case "up", "left", "h", "shift+tab":
		m.chainCursor = (m.chainCursor + len(chainChoices) - 1) % len(chainChoices)
	case "down", "right", "l", "tab":
		m.chainCursor = (m.chainCursor + 1) % len(chainChoices)
	case "enter":
		m.cfg.chain = chainChoices[m.chainCursor]
		m.choosingChain = false
		m.addLog("setting chain saved: " + m.cfg.chain)
	}
	return m, nil
}

func (m model) updateEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.editing = false
		m.editValue = ""
	case "enter":
		if err := m.applyEdit(); err != nil {
			m.err = err.Error()
			m.addLog("invalid setting: " + err.Error())
			return m, nil
		}
		m.editing = false
		m.editValue = ""
		m.addLog("setting " + settingFields[m.selected] + " saved")
	case "backspace", "ctrl+h":
		if len(m.editValue) > 0 {
			m.editValue = m.editValue[:len(m.editValue)-1]
		}
	case "ctrl+u":
		m.editValue = ""
	default:
		if len(msg.String()) == 1 {
			m.editValue += msg.String()
		}
	}
	return m, nil
}

func (m model) selectedValue() string {
	switch settingFields[m.selected] {
	case "chain":
		return m.cfg.chain
	case "start-block":
		return zeroDash(m.cfg.startBlock)
	case "end-block":
		return zeroDash(m.cfg.endBlock)
	case "last":
		return fmt.Sprint(m.cfg.last)
	case "concurrency":
		return fmt.Sprint(m.cfg.concurrency)
	case "min-balance":
		return m.cfg.minBalance
	case "retries":
		return fmt.Sprint(m.cfg.retries)
	case "timeout":
		return m.cfg.timeout.String()
	default:
		return ""
	}
}

func (m *model) applyEdit() error {
	value := strings.TrimSpace(m.editValue)
	switch settingFields[m.selected] {
	case "chain":
		if value == "" {
			return fmt.Errorf("chain cannot be empty")
		}
		m.cfg.chain = value
	case "start-block":
		parsed, err := parseOptionalUint64(value)
		if err != nil {
			return err
		}
		m.cfg.startBlock = parsed
	case "end-block":
		parsed, err := parseOptionalUint64(value)
		if err != nil {
			return err
		}
		m.cfg.endBlock = parsed
	case "last":
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil || parsed == 0 {
			return fmt.Errorf("last must be a positive integer")
		}
		m.cfg.last = parsed
	case "concurrency":
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 64 {
			return fmt.Errorf("concurrency must be 1..64")
		}
		m.cfg.concurrency = parsed
	case "min-balance":
		if _, err := eth.ParseEther(value); err != nil {
			return err
		}
		m.cfg.minBalance = value
	case "retries":
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 || parsed > 20 {
			return fmt.Errorf("retries must be 0..20")
		}
		m.cfg.retries = parsed
	case "timeout":
		parsed, err := time.ParseDuration(value)
		if err != nil || parsed < time.Second {
			return fmt.Errorf("timeout must be a duration like 20s")
		}
		m.cfg.timeout = parsed
	}
	return nil
}

func (m model) logPanel(maxLines int) string {
	lines := []string{titleStyle.Render("Logger")}
	start := 0
	if len(m.logs) > maxLines {
		start = len(m.logs) - maxLines
	}
	lines = append(lines, m.logs[start:]...)
	return strings.Join(lines, "\n")
}

func (m *model) addLog(line string) {
	m.logs = append(m.logs, time.Now().Format("15:04:05")+" "+line)
	if len(m.logs) > 500 {
		m.logs = m.logs[len(m.logs)-500:]
	}
}

func runScraper(cfg config, logs chan<- string, rpcStats chan<- []rpcStat, done chan<- scrapeDone) {
	ctx := context.Background()
	sink, err := storage.OpenPostgres(ctx, cfg.postgresURL, cfg.postgresTable, cfg.resetDB)
	if err != nil {
		done <- scrapeDone{Err: err}
		return
	}
	defer sink.Close()

	chain, err := chains.Get(cfg.chain)
	if err != nil {
		done <- scrapeDone{Err: err}
		return
	}
	rpcStatsRecorder := newRPCStatsRecorder(rpcStats)
	rpcClient, err := rpc.NewClient(
		chain.RPCURLs,
		cfg.timeout,
		rpc.WithRetries(cfg.retries),
		rpc.WithCallObserver(rpcStatsRecorder.record),
	)
	if err != nil {
		done <- scrapeDone{Err: err}
		return
	}
	contractScraper := scraper.New(chain, rpcClient)
	latest, err := contractScraper.LatestBlock(ctx)
	if err != nil {
		done <- scrapeDone{Err: err}
		return
	}
	start, end, err := resolveRange(latest, cfg.startBlock, cfg.endBlock, cfg.last)
	if err != nil {
		done <- scrapeDone{Err: err}
		return
	}
	minBalance, err := eth.ParseEther(cfg.minBalance)
	if err != nil {
		done <- scrapeDone{Err: err}
		return
	}
	var minBalanceWei *big.Int
	if minBalance.Sign() > 0 {
		minBalanceWei = minBalance
	}
	logs <- fmt.Sprintf("scraping %s blocks %d..%d", chain.Name, start, end)
	count, err := contractScraper.ScrapeRangeWithSink(ctx, start, end, sink, scraper.Options{
		Concurrency:     cfg.concurrency,
		MinBalanceWei:   minBalanceWei,
		ContinueOnError: cfg.continueOnError,
		Progress: func(progress scraper.Progress) {
			logs <- fmt.Sprintf("progress %d/%d block=%d contracts=%d", progress.Completed, progress.Total, progress.BlockNumber, progress.Contracts)
		},
		Error: func(blockNumber uint64, err error) {
			logs <- fmt.Sprintf("block %d error: %v", blockNumber, err)
		},
		Found: func(creation scraper.ContractCreation) {
			logs <- fmt.Sprintf("found %s block=%d", creation.ContractAddress, creation.BlockNumber)
		},
	})
	done <- scrapeDone{Count: count, Err: err}
}

type rpcStatsRecorder struct {
	mu      sync.Mutex
	counts  map[string]int64
	updates chan<- []rpcStat
}

func newRPCStatsRecorder(updates chan<- []rpcStat) *rpcStatsRecorder {
	return &rpcStatsRecorder{
		counts:  make(map[string]int64),
		updates: updates,
	}
}

func (c *rpcStatsRecorder) record(url string, calls int64) {
	c.mu.Lock()
	c.counts[url] += calls
	stats := make([]rpcStat, 0, len(c.counts))
	for url, count := range c.counts {
		stats = append(stats, rpcStat{URL: url, Calls: count})
	}
	c.mu.Unlock()
	sort.Slice(stats, func(i int, j int) bool {
		return stats[i].Calls > stats[j].Calls
	})
	if len(stats) > 8 {
		stats = stats[:8]
	}
	select {
	case c.updates <- stats:
	default:
	}
}

func fetchStats(cfg config) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		sink, err := storage.OpenPostgres(ctx, cfg.postgresURL, cfg.postgresTable, false)
		if err != nil {
			return statsMsg{Error: err.Error()}
		}
		_ = sink.Close()
		db, err := sql.Open("postgres", cfg.postgresURL)
		if err != nil {
			return statsMsg{Error: err.Error()}
		}
		defer db.Close()
		if err := db.PingContext(ctx); err != nil {
			return statsMsg{Error: err.Error()}
		}
		stats := dbStats{Connected: true, LastUpdated: time.Now()}
		row := db.QueryRowContext(ctx, fmt.Sprintf(`
SELECT
	count(*),
	count(DISTINCT chain_id),
	COALESCE(max(block_number), 0)
FROM %s
`, cfg.postgresTable))
		if err := row.Scan(&stats.Total, &stats.Chains, &stats.LatestBlock); err != nil {
			stats.Error = err.Error()
			return statsMsg(stats)
		}
		_ = db.QueryRowContext(ctx, fmt.Sprintf(`
SELECT network, block_number, contract_address, created_at::text
FROM %s
ORDER BY id DESC
LIMIT 1
`, cfg.postgresTable)).Scan(&stats.LastNetwork, &stats.LastBlock, &stats.LastContract, &stats.LastInsertedAt)
		return statsMsg(stats)
	}
}

func fetchRecent(cfg config) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		db, err := sql.Open("postgres", cfg.postgresURL)
		if err != nil {
			return recentMsg(nil)
		}
		defer db.Close()
		rows, err := db.QueryContext(ctx, fmt.Sprintf(`
SELECT network, block_number, contract_address
FROM %s
ORDER BY id DESC
LIMIT 8
`, cfg.postgresTable))
		if err != nil {
			return recentMsg(nil)
		}
		defer rows.Close()
		var recent []recentContract
		for rows.Next() {
			var item recentContract
			if err := rows.Scan(&item.Network, &item.Block, &item.Address); err == nil {
				recent = append(recent, item)
			}
		}
		return recentMsg(recent)
	}
}

func fetchTopRPCs(cfg config) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		db, err := sql.Open("postgres", cfg.postgresURL)
		if err != nil {
			return rpcStatsMsg(nil)
		}
		defer db.Close()
		rows, err := db.QueryContext(ctx, fmt.Sprintf(`
SELECT rpc_url, count(*) AS calls
FROM %s
GROUP BY rpc_url
ORDER BY calls DESC
LIMIT 8
`, cfg.postgresTable))
		if err != nil {
			return rpcStatsMsg(nil)
		}
		defer rows.Close()
		var stats []rpcStat
		for rows.Next() {
			var item rpcStat
			if err := rows.Scan(&item.URL, &item.Calls); err == nil {
				stats = append(stats, item)
			}
		}
		return rpcStatsMsg(stats)
	}
}

func normalizeTableName(table string) (string, error) {
	table = strings.TrimSpace(table)
	if table == "" {
		table = "contract_creations"
	}
	if !isValidTableName(table) {
		return "", fmt.Errorf("invalid postgres table name %q", table)
	}
	return table, nil
}

func tick() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func drainLogs(ch <-chan string) tea.Cmd {
	return func() tea.Msg {
		select {
		case line := <-ch:
			return logMsg(line)
		case <-time.After(250 * time.Millisecond):
			return logMsg("")
		}
	}
}

func drainRPCStats(ch <-chan []rpcStat) tea.Cmd {
	return func() tea.Msg {
		select {
		case stats := <-ch:
			return rpcStatsMsg(stats)
		case <-time.After(250 * time.Millisecond):
			return rpcStatsPollMsg{}
		}
	}
}

func waitDone(ch <-chan scrapeDone) tea.Cmd {
	return func() tea.Msg {
		result := <-ch
		return doneMsg(result)
	}
}

func env(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

type envInfo struct {
	Path           string
	Loaded         bool
	HasPostgresURL bool
}

func loadDotEnv(path string) envInfo {
	info := envInfo{Path: path}
	file, err := os.Open(path)
	if err != nil {
		return info
	}
	defer file.Close()
	info.Loaded = true
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key == "POSTGRES_URL" && strings.TrimSpace(value) != "" {
			info.HasPostgresURL = true
		}
		if key == "" || os.Getenv(key) != "" {
			continue
		}
		_ = os.Setenv(key, value)
	}
	return info
}

func cycleString(values []string, current string, delta int) string {
	if len(values) == 0 {
		return current
	}
	index := 0
	for i, value := range values {
		if value == current {
			index = i
			break
		}
	}
	index = (index + delta) % len(values)
	if index < 0 {
		index += len(values)
	}
	return values[index]
}

func chainChoiceIndex(current string) int {
	for i, value := range chainChoices {
		if value == current {
			return i
		}
	}
	return 0
}

func adjustUint64(current uint64, delta int, step uint64) uint64 {
	if delta < 0 {
		decrement := step * uint64(-delta)
		if current <= decrement {
			return 0
		}
		return current - decrement
	}
	return current + step*uint64(delta)
}

func parseOptionalUint64(value string) (uint64, error) {
	if value == "" || value == "-" {
		return 0, nil
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("block must be an integer or empty")
	}
	return parsed, nil
}

func zeroDash(value uint64) string {
	if value == 0 {
		return "-"
	}
	return fmt.Sprint(value)
}

func resolveRange(latest uint64, start uint64, end uint64, last uint64) (uint64, uint64, error) {
	if start != 0 || end != 0 {
		if start == 0 || end == 0 {
			return 0, 0, fmt.Errorf("start block and end block must both be set")
		}
		if start > end {
			return 0, 0, fmt.Errorf("start block must be <= end block")
		}
		return start, end, nil
	}
	if last == 0 || last > latest+1 {
		return 0, 0, fmt.Errorf("last is bigger than latest block range")
	}
	return latest - last + 1, latest, nil
}

func trimMiddle(value string, maxLen int) string {
	if len(value) <= maxLen {
		return value
	}
	if maxLen <= 3 {
		return value[:maxLen]
	}
	left := (maxLen - 3) / 2
	right := maxLen - 3 - left
	return value[:left] + "..." + value[len(value)-right:]
}

func isValidTableName(table string) bool {
	if strings.TrimSpace(table) == "" {
		return false
	}
	for _, r := range table {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_') {
			return false
		}
	}
	return true
}

func isPostgresURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return parsed.Scheme == "postgres" || parsed.Scheme == "postgresql"
}

func maskURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return trimMiddle(raw, 48)
	}
	if parsed.User != nil {
		username := parsed.User.Username()
		if username != "" {
			parsed.User = url.UserPassword(username, "****")
		}
	}
	return trimMiddle(parsed.String(), 56)
}

func splitWidths(total int, gap int, columns int) []int {
	if columns <= 0 {
		return nil
	}
	available := total - gap*(columns-1)
	if available < columns {
		available = columns
	}
	base := available / columns
	remainder := available % columns
	widths := make([]int, columns)
	for i := range widths {
		widths[i] = base
		if i < remainder {
			widths[i]++
		}
	}
	return widths
}

func clamp(value int, minValue int, maxValue int) int {
	if maxValue < minValue {
		maxValue = minValue
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func max(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
