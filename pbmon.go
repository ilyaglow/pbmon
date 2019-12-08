package pbmon

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/url"
	"time"

	"github.com/allegro/bigcache"
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
	cache    *bigcache.BigCache
	timeout  time.Duration
	pbClient *pastebin.PastebinClient
	OnNew    OnNewPaste
}

// New constructs a pastebin monitor.
func (p *PastebinMonitor) New(opts ...func(*PastebinMonitor) error) (*PastebinMonitor, error) {
	cache, err := bigcache.NewBigCache(bigcache.DefaultConfig(defaultEvictionDuration))
	if err != nil {
		return nil, err
	}

	baseURL, _ := url.Parse("https://scrape.pastebin.com/")
	pc := pastebin.New(baseURL)

	conf := &PastebinMonitor{
		pbClient: pc,
		cache:    cache,
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
	var newPastes []pastebin.Paste
	pastes, err := p.recent(recentSize)
	if err != nil {
		return nil, err
	}

	for _, paste := range pastes {
		v, err := p.cache.Get(paste.Key)
		switch err {
		case nil:
			if string(v) == paste.Date.String() {
				continue
			}
			err = p.cache.Set(paste.Key, []byte(paste.Date.String()))
			if err != nil {
				return newPastes, err
			}
			newPastes = append(newPastes, paste)
		case bigcache.ErrEntryNotFound:
			err = p.cache.Set(paste.Key, []byte(paste.Date.String()))
			if err != nil {
				return newPastes, err
			}
			newPastes = append(newPastes, paste)
		default:
			return newPastes, err
		}
	}

	return newPastes, nil
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
