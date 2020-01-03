// Package pbmon aims to be a simple handy abstraction based on
// github.com/dutchcoders/gopastebin. It allows user to do whatever she wants
// with a new available paste.
package pbmon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"time"

	pastebin "github.com/dutchcoders/gopastebin"
)

const (
	DefaultRecentSize       = 50 // Amount of pastes to get on one request
	defaultEvictionDuration = 10 * time.Minute
	pasteIDLen              = 9
	DefaultTimeout          = 10 * time.Second // Timeout between poll requests.
)

// OnNewPaste is a callback function for processing a new paste.
type OnNewPaste func(pastebin.Paste, io.ReadCloser) error

// PastebinMonitor handles monitoring of the pastebin.com
type PastebinMonitor struct {
	topKey    string
	timeout   time.Duration
	pbClient  *pastebin.PastebinClient
	stateFile *os.File
	OnNew     OnNewPaste
}

// New constructs a pastebin monitor.
func New(opts ...func(*PastebinMonitor) error) (*PastebinMonitor, error) {
	baseURL, _ := url.Parse("https://scrape.pastebin.com/")
	pc := pastebin.New(baseURL)

	conf := &PastebinMonitor{
		pbClient: pc,
		timeout:  DefaultTimeout,
		OnNew: func(p pastebin.Paste, r io.ReadCloser) error {
			log.Printf("title=%s user=%s syntax=%s url=%s ", p.Title, p.User, p.Syntax, p.FullURL)
			return nil
		},
	}

	for _, f := range opts {
		err := f(conf)
		if err != nil {
			return nil, err
		}
	}

	return conf, conf.loadState()
}

func (p *PastebinMonitor) loadState() error {
	var err error

	if p.stateFile != nil {
		p.topKey, err = readState(p.stateFile)
		if err != nil {
			return err
		}
		return nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home directory: %w", err)
	}
	stateFileName := filepath.Join(home, ".pbmon")

	f, err := os.OpenFile(stateFileName, os.O_RDWR|os.O_CREATE, 0744)
	p.topKey, err = readState(f)
	if err != nil {
		return err
	}

	p.stateFile = f
	return err
}

func readState(r io.Reader) (string, error) {
	top := make([]byte, pasteIDLen)
	_, err := r.Read(top)
	if err == io.EOF {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read state file: %w", err)
	}
	return string(top), nil
}

// SetStateFile to be able to resume execution on the last paste and achieve
// persistence.
func SetStateFile(fullLoc string) func(*PastebinMonitor) error {
	f, err := os.OpenFile(fullLoc, os.O_RDWR|os.O_CREATE, 0755)
	if err != nil {
		return func(p *PastebinMonitor) error {
			return err
		}
	}
	return func(p *PastebinMonitor) error {
		p.stateFile = f
		return nil
	}
}

// Do starts fetching new pastes.
func (p *PastebinMonitor) Do(recentSize int, timeout time.Duration) error {
	err := p.do(recentSize, timeout)
	if err != nil {
		return err
	}

	t := time.NewTicker(timeout)
	for range t.C {
		err = p.do(recentSize, timeout)
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *PastebinMonitor) do(recentSize int, timeout time.Duration) error {
	pastes, err := p.fetchNewPastes(recentSize)
	if err != nil {
		return fmt.Errorf("fetch pastes: %w", err)
	}

	for i := len(pastes) - 1; i >= 0; i-- {
		err := p.processPaste(pastes[i])
		if err != nil {
			return fmt.Errorf("process paste: %w", err)
		}

		err = p.stateFile.Truncate(0)
		if err != nil {
			return fmt.Errorf("truncate %s: %w", p.stateFile.Name(), err)
		}

		_, err = p.stateFile.Seek(0, 0)
		if err != nil {
			return fmt.Errorf("seek to the beginning of %s: %w", p.stateFile.Name(), err)
		}

		_, err = p.stateFile.WriteString(pastes[i].Key)
		if err != nil {
			return fmt.Errorf("save state to %s: %w", p.stateFile.Name(), err)
		}

		p.topKey = pastes[i].Key
	}
	return nil
}

func (p *PastebinMonitor) fetchNewPastes(recentSize int) ([]pastebin.Paste, error) {
	pastes, err := p.recent(recentSize)
	if err != nil {
		return nil, fmt.Errorf("recent pastes: %w", err)
	}

	if len(pastes) == 0 {
		return nil, errors.New("no pastes available")
	}

	if p.topKey == "" { // it's a first run, so nothing new
		p.topKey = pastes[0].Key
		return nil, nil
	}

	matchPos := len(pastes) - 1
	for i := range pastes {
		if pastes[i].Key == p.topKey {
			matchPos = i
		}
	}
	return pastes[0:matchPos], nil
}

func (p *PastebinMonitor) processPaste(paste pastebin.Paste) error {
	body, err := p.pbClient.GetRaw(paste.Key)
	if err != nil {
		return fmt.Errorf("pastebin.GetRaw: %w", err)
	}

	return p.OnNew(paste, body)
}

func (p *PastebinMonitor) recent(size int) ([]pastebin.Paste, error) {
	req, err := p.pbClient.NewRequest("GET", fmt.Sprintf("/api_scraping.php?limit=%d", size))
	if err != nil {
		return nil, err
	}

	resp, err := p.pbClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request to %s: %w", req.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s returned status code %d", req.URL, resp.StatusCode)
	}
	pastes := []pastebin.Paste{}

	err = json.NewDecoder(resp.Body).Decode(&pastes)
	if err != nil {
		return nil, fmt.Errorf("decode json: %w", err)
	}

	return pastes, nil
}
