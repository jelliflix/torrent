package torrent

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jelliflix/meta"
	"github.com/tidwall/gjson"
)

var trackersTPB = []string{
	"udp://tracker.coppersurfer.tk:6969/announce",
	"udp://9.rarbg.to:2920/announce",
	"udp://tracker.opentrackr.org:1337",
	"udp://tracker.internetwarriors.net:1337/announce",
	"udp://tracker.leechers-paradise.org:6969/announce",
	"udp://tracker.coppersurfer.tk:6969/announce",
	"udp://tracker.pirateparty.gr:6969/announce",
	"udp://tracker.cyberia.is:6969/announce",
}

type TPBOptions struct {
	BaseURL        string
	SocksProxyAddr string
	Timeout        time.Duration
	CacheAge       time.Duration
}

var DefaultTPBOpts = TPBOptions{
	BaseURL:  "https://apibay.org",
	Timeout:  5 * time.Second,
	CacheAge: 24 * time.Hour,
}

var _ Client = (*tpb)(nil)

type tpb struct {
	http     *http.Client
	meta     *meta.Cinemeta
	cache    Cache
	baseURL  string
	cacheAge time.Duration
}

func NewTPB(opts TPBOptions, cache Cache, meta *meta.Cinemeta) *tpb {
	return &tpb{
		http:     &http.Client{Timeout: opts.Timeout},
		cacheAge: opts.CacheAge,
		baseURL:  opts.BaseURL,
		cache:    cache,
		meta:     meta,
	}
}

func (c *tpb) FindMovie(ctx context.Context, imdbID string) ([]Result, error) {
	movie, err := c.meta.GetMovie(imdbID)
	if err != nil {
		return nil, fmt.Errorf("couldn't get movie title via Cinemeta for ID %v: %v", imdbID, err)
	}
	escapedQuery := imdbID
	return c.find(ctx, imdbID, movie.Name, escapedQuery, false)
}

func (c *tpb) FindEpisode(ctx context.Context, imdbID, title string) ([]Result, error) {
	series, err := c.meta.GetSeries(imdbID)
	if err != nil {
		return nil, fmt.Errorf("couldn't get TV series title via Cinemeta for ID %v: %v", imdbID, err)
	}

	if series.Name != "" {
		title = series.Name + " " + title
	}

	escapedQuery := url.QueryEscape(title) + "&cat=208"
	return c.find(ctx, imdbID, title, escapedQuery, true)
}

func (c *tpb) find(ctx context.Context, id, title, escapedQuery string, fuzzy bool) ([]Result, error) {
	cacheKey := id + "-TPB"
	torrentList, created, found, err := c.cache.Get(cacheKey)
	if found && time.Since(created) <= (c.cacheAge) {
		return torrentList, nil
	}

	reqUrl := c.baseURL + "/q.php?q=" + escapedQuery
	res, err := c.http.Get(reqUrl)
	if err != nil {
		return nil, fmt.Errorf("couldn't GET %v: %v", reqUrl, err)
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

	torrents := gjson.ParseBytes(resBody).Array()
	if len(torrents) == 0 {
		return nil, nil
	}

	var results []Result
	for _, torrent := range torrents {
		torrentName := torrent.Get("name").String()
		quality := ""
		if strings.Contains(torrentName, "720p") {
			quality = "720p"
		} else if strings.Contains(torrentName, "1080p") {
			quality = "1080p"
		} else if strings.Contains(torrentName, "2160p") {
			quality = "2160p"
		} else {
			continue
		}
		if strings.Contains(torrentName, "10bit") {
			quality += " 10bit"
		}
		if strings.Contains(torrentName, "HDCAM") {
			quality += " (cam)"
		} else if strings.Contains(torrentName, "HDTS") || strings.Contains(torrentName, "HD-TS") {
			quality += " (telesync)"
		}
		infoHash := torrent.Get("info_hash").String()
		if infoHash == "" {
			continue
		} else if len(infoHash) != 40 {
			continue
		}
		infoHash = strings.ToLower(infoHash)
		magnetURL := createMagnetURL(ctx, infoHash, title, trackersTPB)
		size := int(torrent.Get("size").Int())
		seeders := int(torrent.Get("seeders").Int())
		result := Result{
			Name:      torrentName,
			Quality:   quality,
			InfoHash:  infoHash,
			MagnetURL: magnetURL,
			Fuzzy:     fuzzy,
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
