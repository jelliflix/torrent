package torrent

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/tidwall/gjson"
)

type RARBGOptions struct {
	BaseURL  string
	Timeout  time.Duration
	CacheAge time.Duration
}

var DefaultRARBOpts = RARBGOptions{
	BaseURL:  "https://torrentapi.org",
	Timeout:  5 * time.Second,
	CacheAge: 24 * time.Hour,
}

var _ Client = (*rarbg)(nil)

type rarbg struct {
	baseURL      string
	httpClient   *http.Client
	cache        Cache
	cacheAge     time.Duration
	token        string
	tokenExpired func() bool
	lastRequest  time.Time
	lock         *sync.Mutex
}

func NewRARBG(opts RARBGOptions, cache Cache) *rarbg {
	return &rarbg{
		baseURL: opts.BaseURL,
		httpClient: &http.Client{
			Timeout: opts.Timeout,
		},
		cache:        cache,
		cacheAge:     opts.CacheAge,
		tokenExpired: func() bool { return true },
		lock:         &sync.Mutex{},
	}
}

func (c *rarbg) FindMovie(ctx context.Context, imdbID string) ([]Result, error) {
	escapedQuery := "search_imdb=" + imdbID
	return c.find(ctx, imdbID, escapedQuery)
}

func (c *rarbg) FindEpisode(ctx context.Context, imdbID, title string) ([]Result, error) {
	escapedQuery := "search_string=" + url.QueryEscape(title)
	return c.find(ctx, imdbID, escapedQuery)
}

func (c *rarbg) find(_ context.Context, id, escapedQuery string) ([]Result, error) {
	cacheKey := id + "-RARBG"
	torrentList, created, found, err := c.cache.Get(cacheKey)
	if found && time.Since(created) <= (c.cacheAge) {
		return torrentList, nil
	}

	if c.tokenExpired() {
		if err = c.RefreshToken(); err != nil {
			return nil, fmt.Errorf("couldn't refresh token: %v", err)
		}
	}

	c.lock.Lock()
	time.Sleep(2*time.Second - time.Since(c.lastRequest))
	defer func() {
		c.lock.Unlock()
		c.lastRequest = time.Now()
	}()

	URL := c.baseURL + "/pubapi_v2.php?app_id=jelliflix&mode=search&sort=seeders&format=json_extended&ranked=0&token=" + c.token + "&" + escapedQuery
	req, err := http.NewRequest("GET", URL, nil)
	if err != nil {
		return nil, fmt.Errorf("couldn't create request: %v", err)
	}
	req.Header.Set("User-Agent", "curl/7.47.0")
	req.Header.Set("Accept", "*/*")
	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't GET %v: %v", URL, err)
	}
	defer func() {
		_ = res.Body.Close()
	}()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad GET response: %v", res.StatusCode)
	}
	resBody, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("couldn't read response body: %v", err)
	}

	torrents := gjson.GetBytes(resBody, "torrent_results").Array()
	if len(torrents) == 0 {
		return nil, nil
	}
	var results []Result
	for _, torrent := range torrents {
		filename := torrent.Get("title").String()

		quality := ""
		if strings.Contains(filename, "720p") {
			quality = "720p"
		} else if strings.Contains(filename, "1080p") {
			quality = "1080p"
		} else if strings.Contains(filename, "2160p") {
			quality = "2160p"
		} else {
			continue
		}

		magnet := torrent.Get("download").String()

		match := magnet2InfoHashRegex.Find([]byte(magnet))
		infoHash := strings.TrimPrefix(string(match), "btih:")
		infoHash = strings.TrimSuffix(infoHash, "&")
		infoHash = strings.ToLower(infoHash)
		if len(infoHash) != 40 {
			continue
		}
		size := int(torrent.Get("size").Int())
		seeders := int(torrent.Get("seeders").Int())

		result := Result{
			Name:      filename,
			Quality:   quality,
			InfoHash:  infoHash,
			MagnetURL: magnet,
			Size:      size,
			Seeders:   seeders,
		}
		results = append(results, result)
	}

	if err := c.cache.Set(cacheKey, results); err != nil {
		return results, fmt.Errorf("couldn't cache torrents: %v", err)
	}

	return results, nil
}

func (c *rarbg) RefreshToken() error {
	URL := c.baseURL + "/pubapi_v2.php?app_id=jelliflix&get_token=get_token"
	req, err := http.NewRequest("GET", URL, nil)
	if err != nil {
		return fmt.Errorf("couldn't create request object: %v", req)
	}

	c.lock.Lock()
	time.Sleep(2*time.Second - time.Since(c.lastRequest))
	defer func() {
		c.lock.Unlock()
		c.lastRequest = time.Now()
	}()
	if !c.tokenExpired() {
		return nil
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't GET %v: %v", URL, err)
	}
	defer func() {
		_ = res.Body.Close()
	}()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("bad GET response: %v", res.StatusCode)
	}
	resBody, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("couldn't read response body: %v", err)
	}
	token := gjson.GetBytes(resBody, "token").String()
	if token == "" {
		return fmt.Errorf("token is empty")
	}
	c.token = token
	createdAt := time.Now()
	c.tokenExpired = func() bool {
		return time.Since(createdAt).Minutes() > 14
	}
	return nil
}
