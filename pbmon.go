package pbmon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"time"

	pastebin "github.com/dutchcoders/gopastebin"
)

const (
	DefaultRecentSize       = 50
	defaultEvictionDuration = 10 * time.Minute
	DefaultTimeout          = 10 * time.Second
)

// OnNewPaste is a callback function for processing a new paste.
type OnNewPaste func(pastebin.Paste, io.ReadCloser) error

// PastebinMonitor handles monitoring of the pastebin.com
type PastebinMonitor struct {
	topKey   string
	timeout  time.Duration
	pbClient *pastebin.PastebinClient
	OnNew    OnNewPaste
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

	return conf, nil
}

// Do starts fetching new pastes.
// It doesn't care about old pastes, so you will get pastes callbacks after a
// specified timeout passed.
func (p *PastebinMonitor) Do(recentSize int, timeout time.Duration) error {
	_, err := p.fetchNewPastes(recentSize)
	if err != nil {
		return err
	}

	t := time.NewTicker(timeout)
	for range t.C {
		pastes, err := p.fetchNewPastes(recentSize)
		if err != nil {
			return err
		}

		for _, pp := range pastes {
			err := p.processPaste(pp)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *PastebinMonitor) fetchNewPastes(recentSize int) ([]pastebin.Paste, error) {
	pastes, err := p.recent(recentSize)
	if err != nil {
		return nil, err
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
	p.topKey = pastes[0].Key

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
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s returned status code %d", req.URL, resp.StatusCode)
	}

	pastes := []pastebin.Paste{}

	err = json.NewDecoder(resp.Body).Decode(&pastes)
	if err != nil {
		return nil, err
	}

	return pastes, nil
}
