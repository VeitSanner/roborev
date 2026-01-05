package tui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/wesm/roborev/internal/storage"
)

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205")).
			MarginBottom(1)

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("212"))

	queuedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("226"))
	runningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("33"))
	doneStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("46"))
	failedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			MarginTop(1)
)

type view int

const (
	viewQueue view = iota
	viewReview
)

// Model is the TUI model for the review queue
type Model struct {
	serverAddr    string
	jobs          []storage.ReviewJob
	status        storage.DaemonStatus
	selectedIdx   int
	currentView   view
	currentReview *storage.Review
	reviewScroll  int
	width         int
	height        int
	err           error
}

type tickMsg time.Time
type jobsMsg []storage.ReviewJob
type statusMsg storage.DaemonStatus
type reviewMsg *storage.Review
type errMsg error

// NewModel creates a new TUI model
func NewModel(serverAddr string) Model {
	return Model{
		serverAddr:  serverAddr,
		jobs:        []storage.ReviewJob{},
		currentView: viewQueue,
		width:       80,
		height:      24,
	}
}

// Init implements tea.Model
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tea.WindowSize(),
		m.tick(),
		m.fetchJobs(),
		m.fetchStatus(),
	)
}

func (m Model) tick() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) fetchJobs() tea.Cmd {
	return func() tea.Msg {
		resp, err := http.Get(m.serverAddr + "/api/jobs?limit=50")
		if err != nil {
			return errMsg(err)
		}
		defer resp.Body.Close()

		var result struct {
			Jobs []storage.ReviewJob `json:"jobs"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return errMsg(err)
		}
		return jobsMsg(result.Jobs)
	}
}

func (m Model) fetchStatus() tea.Cmd {
	return func() tea.Msg {
		resp, err := http.Get(m.serverAddr + "/api/status")
		if err != nil {
			return errMsg(err)
		}
		defer resp.Body.Close()

		var status storage.DaemonStatus
		if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
			return errMsg(err)
		}
		return statusMsg(status)
	}
}

func (m Model) fetchReview(jobID int64) tea.Cmd {
	return func() tea.Msg {
		resp, err := http.Get(fmt.Sprintf("%s/api/review?job_id=%d", m.serverAddr, jobID))
		if err != nil {
			return errMsg(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound {
			return errMsg(fmt.Errorf("no review found"))
		}

		var review storage.Review
		if err := json.NewDecoder(resp.Body).Decode(&review); err != nil {
			return errMsg(err)
		}
		return reviewMsg(&review)
	}
}

// Update implements tea.Model
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.currentView == viewReview {
				m.currentView = viewQueue
				m.currentReview = nil
				m.reviewScroll = 0
				return m, nil
			}
			return m, tea.Quit

		case "up", "k":
			if m.currentView == viewQueue {
				if m.selectedIdx > 0 {
					m.selectedIdx--
				}
			} else {
				if m.reviewScroll > 0 {
					m.reviewScroll--
				}
			}

		case "down", "j":
			if m.currentView == viewQueue {
				if m.selectedIdx < len(m.jobs)-1 {
					m.selectedIdx++
				}
			} else {
				m.reviewScroll++
			}

		case "enter":
			if m.currentView == viewQueue && len(m.jobs) > 0 {
				job := m.jobs[m.selectedIdx]
				if job.Status == storage.JobStatusDone {
					return m, m.fetchReview(job.ID)
				} else if job.Status == storage.JobStatusFailed {
					m.currentReview = &storage.Review{
						Agent:  job.Agent,
						Output: "Job failed:\n\n" + job.Error,
						Job:    &job,
					}
					m.currentView = viewReview
					m.reviewScroll = 0
					return m, nil
				}
			}

		case "esc":
			if m.currentView == viewReview {
				m.currentView = viewQueue
				m.currentReview = nil
				m.reviewScroll = 0
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tickMsg:
		return m, tea.Batch(m.tick(), m.fetchJobs(), m.fetchStatus())

	case jobsMsg:
		m.jobs = msg
		if m.selectedIdx >= len(m.jobs) {
			m.selectedIdx = max(0, len(m.jobs)-1)
		}

	case statusMsg:
		m.status = storage.DaemonStatus(msg)

	case reviewMsg:
		m.currentReview = msg
		m.currentView = viewReview
		m.reviewScroll = 0

	case errMsg:
		m.err = msg
	}

	return m, nil
}

// View implements tea.Model
func (m Model) View() string {
	if m.currentView == viewReview && m.currentReview != nil {
		return m.renderReviewView()
	}
	return m.renderQueueView()
}

func (m Model) renderQueueView() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("RoboRev Queue"))
	b.WriteString("\n")

	statusLine := fmt.Sprintf("Workers: %d/%d | Queued: %d | Running: %d | Done: %d | Failed: %d | Size: %dx%d",
		m.status.ActiveWorkers, m.status.MaxWorkers,
		m.status.QueuedJobs, m.status.RunningJobs,
		m.status.CompletedJobs, m.status.FailedJobs,
		m.width, m.height)
	b.WriteString(statusStyle.Render(statusLine))
	b.WriteString("\n\n")

	if len(m.jobs) == 0 {
		b.WriteString("No jobs in queue\n")
	} else {
		header := fmt.Sprintf("  %-4s %-17s %-15s %-12s %-8s %s",
			"ID", "Ref", "Repo", "Agent", "Status", "Time")
		b.WriteString(statusStyle.Render(header))
		b.WriteString("\n")
		b.WriteString("  " + strings.Repeat("-", min(m.width-4, 78)))
		b.WriteString("\n")

		reservedLines := 9
		visibleJobs := m.height - reservedLines
		if visibleJobs < 3 {
			visibleJobs = 3
		}

		start := 0
		end := len(m.jobs)

		if len(m.jobs) > visibleJobs {
			start = m.selectedIdx - visibleJobs/2
			if start < 0 {
				start = 0
			}
			end = start + visibleJobs
			if end > len(m.jobs) {
				end = len(m.jobs)
				start = end - visibleJobs
			}
		}

		for i := start; i < end; i++ {
			job := m.jobs[i]
			line := m.renderJobLine(job)
			if i == m.selectedIdx {
				line = selectedStyle.Render("> " + line)
			} else {
				line = "  " + line
			}
			b.WriteString(line)
			b.WriteString("\n")
		}

		if len(m.jobs) > visibleJobs {
			scrollInfo := fmt.Sprintf("[showing %d-%d of %d]", start+1, end, len(m.jobs))
			b.WriteString(statusStyle.Render(scrollInfo))
			b.WriteString("\n")
		}
	}

	b.WriteString(helpStyle.Render("up/down: navigate | enter: view review | q: quit"))

	return b.String()
}

func (m Model) renderJobLine(job storage.ReviewJob) string {
	ref := job.GitRef
	if len(ref) > 17 {
		ref = ref[:17]
	}

	repo := job.RepoName
	if len(repo) > 15 {
		repo = repo[:12] + "..."
	}

	agent := job.Agent
	if len(agent) > 12 {
		agent = agent[:12]
	}

	elapsed := ""
	if job.StartedAt != nil {
		if job.FinishedAt != nil {
			elapsed = job.FinishedAt.Sub(*job.StartedAt).Round(time.Second).String()
		} else {
			elapsed = time.Since(*job.StartedAt).Round(time.Second).String()
		}
	}

	status := string(job.Status)
	var styledStatus string
	switch job.Status {
	case storage.JobStatusQueued:
		styledStatus = queuedStyle.Render(status)
	case storage.JobStatusRunning:
		styledStatus = runningStyle.Render(status)
	case storage.JobStatusDone:
		styledStatus = doneStyle.Render(status)
	case storage.JobStatusFailed:
		styledStatus = failedStyle.Render(status)
	default:
		styledStatus = status
	}
	padding := 8 - len(status)
	if padding > 0 {
		styledStatus += strings.Repeat(" ", padding)
	}

	return fmt.Sprintf("%-4d %-17s %-15s %-12s %s %s",
		job.ID, ref, repo, agent, styledStatus, elapsed)
}

func wrapText(text string, width int) []string {
	if width <= 0 {
		width = 100
	}

	var result []string
	for _, line := range strings.Split(text, "\n") {
		if len(line) <= width {
			result = append(result, line)
			continue
		}

		for len(line) > width {
			breakPoint := width
			for i := width; i > width/2; i-- {
				if i < len(line) && line[i] == ' ' {
					breakPoint = i
					break
				}
			}

			result = append(result, line[:breakPoint])
			line = strings.TrimLeft(line[breakPoint:], " ")
		}
		if len(line) > 0 {
			result = append(result, line)
		}
	}

	return result
}

func (m Model) renderReviewView() string {
	var b strings.Builder

	review := m.currentReview
	if review.Job != nil {
		ref := review.Job.GitRef
		if len(ref) > 17 {
			ref = ref[:17]
		}
		title := fmt.Sprintf("Review: %s (%s)", ref, review.Agent)
		b.WriteString(titleStyle.Render(title))
	} else {
		b.WriteString(titleStyle.Render("Review"))
	}
	b.WriteString("\n")

	wrapWidth := min(m.width-2, 100)
	lines := wrapText(review.Output, wrapWidth)

	visibleLines := m.height - 5

	start := m.reviewScroll
	if start >= len(lines) {
		start = max(0, len(lines)-1)
	}
	end := min(start+visibleLines, len(lines))

	for i := start; i < end; i++ {
		b.WriteString(lines[i])
		b.WriteString("\n")
	}

	if len(lines) > visibleLines {
		scrollInfo := fmt.Sprintf("[%d-%d of %d lines]", start+1, end, len(lines))
		b.WriteString(statusStyle.Render(scrollInfo))
		b.WriteString("\n")
	}

	b.WriteString(helpStyle.Render("up/down: scroll | esc/q: back"))

	return b.String()
}
