package torrent

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var magnet2InfoHashRegex = regexp.MustCompile(`btih:.+?&`)

type findFunc func(context.Context, Client) ([]Result, error)

type Client interface {
	FindMovie(ctx context.Context, imdbID string) ([]Result, error)
	FindEpisode(ctx context.Context, imdbID, title string) ([]Result, error)
}

type Torrent struct {
	clients []Client
	timeout time.Duration
}

func NewTorrent(clients []Client, timeout time.Duration) *Torrent {
	return &Torrent{
		clients: clients,
		timeout: timeout,
	}
}

func (t *Torrent) FindMovie(ctx context.Context, imdbID string) ([]Result, error) {
	find := func(ctx context.Context, client Client) ([]Result, error) {
		return client.FindMovie(ctx, imdbID)
	}
	return t.find(ctx, find)
}

func (t *Torrent) FindEpisode(ctx context.Context, imdbID, title string) ([]Result, error) {
	find := func(ctx context.Context, client Client) ([]Result, error) {
		return client.FindEpisode(ctx, imdbID, title)
	}
	return t.find(ctx, find)
}

func (t *Torrent) find(ctx context.Context, find findFunc) ([]Result, error) {
	clients := len(t.clients)
	errChan := make(chan error, clients)
	resChan := make(chan []Result, clients)

	for _, client := range t.clients {
		timer := time.NewTimer(t.timeout)
		go func(client Client, timer *time.Timer) {
			defer timer.Stop()

			siteResChan := make(chan []Result)
			siteErrChan := make(chan error)
			go func() {
				results, err := find(ctx, client)
				if err != nil {
					siteErrChan <- err
				} else {
					siteResChan <- results
				}
			}()
			select {
			case res := <-siteResChan:
				resChan <- res
			case err := <-siteErrChan:
				errChan <- err
			case <-timer.C:
				resChan <- nil
			}
		}(client, timer)
	}

	var combinedResults []Result
	var errs []error
	dupRemovalRequired := false
	for i := 0; i < clients; i++ {
		select {
		case results := <-resChan:
			if !dupRemovalRequired && len(combinedResults) > 0 && len(results) > 0 {
				dupRemovalRequired = true
			}
			combinedResults = append(combinedResults, results...)
		case err := <-errChan:
			errs = append(errs, err)
		}
	}

	returnErrors := len(errs) == clients

	if returnErrors {
		errsMsg := "couldn't find torrents on any site: "
		for i := 1; i <= clients; i++ {
			errsMsg += fmt.Sprintf("%v.: %v; ", i, errs[i-1])
		}
		errsMsg = strings.TrimSuffix(errsMsg, "; ")
		return nil, fmt.Errorf(errsMsg)
	}

	var noDupResults []Result
	if dupRemovalRequired {
		infoHashes := map[string]struct{}{}
		for _, result := range combinedResults {
			if _, ok := infoHashes[result.InfoHash]; !ok {
				noDupResults = append(noDupResults, result)
				infoHashes[result.InfoHash] = struct{}{}
			}
		}
	} else {
		noDupResults = combinedResults
	}

	return noDupResults, nil
}

type Result struct {
	Name      string
	Title     string
	Quality   string
	InfoHash  string
	MagnetURL string

	Seeders int
	Fuzzy   bool
	Size    int
}

func createMagnetURL(_ context.Context, infoHash, title string, trackers []string) string {
	magnetURL := "magnet:?xt=urn:btih:" + infoHash + "&dn=" + url.QueryEscape(title)
	for _, tracker := range trackers {
		magnetURL += "&tr" + tracker
	}
	return magnetURL
}
