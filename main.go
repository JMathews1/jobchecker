package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gocolly/colly"
)

/*─────────────────────────  CONFIG  ─────────────────────────*/

var (
	slackToken = os.Getenv("SLACK_BOT_TOKEN")  // xoxb-…
	channelID  = os.Getenv("SLACK_CHANNEL_ID") // Cxxxxxxxx
)

const (
	historyFile = "history.json" // dedup store (kept)
	dedupWindow = 72 * time.Hour // no repeat alerts within 3 days
	userAgent   = "Mozilla/5.0"
)

/*─────────────────────────  STATE  ─────────────────────────*/

var (
	history    = map[string]int64{} // hash → unix timestamp
	newAlerts  bool                 // set true when at least one alert sent
	historyMtx sync.Mutex
)

/*─────────────────────────  INIT  ─────────────────────────*/

func init() {
	if slackToken == "" || channelID == "" {
		log.Fatal("SLACK_BOT_TOKEN or SLACK_CHANNEL_ID not set")
	}
	if data, err := os.ReadFile(historyFile); err == nil {
		_ = json.Unmarshal(data, &history)
	}
}

/*─────────────────────────  MAIN  ─────────────────────────*/

func main() {
	scrapeAll()
	if !newAlerts {
		noMsg := "No new openings found"
		fmt.Println(noMsg)
		sendSlack(noMsg)
	}
}

/*─────────────────────────  SCRAPER LIST  ─────────────────────────*/

type JobSite struct {
	Name    string
	URL     string
	Scraper func(name, url string)
}

func scrapeAll() {
	var wg sync.WaitGroup

	sites := []JobSite{
		/* consulting / services */
		{"RBC", "https://jobs.rbc.com/ca/en/search-results?keywords=devops&location=Halifax", scrapeRBC},
		{"Scotiabank", "https://jobs.scotiabank.com/search/?q=devops&location=Halifax", scrapeGeneric},
		{"TD Bank", "https://jobs.td.com/en-CA/search-results/?keywords=devops&location=Halifax", scrapeGeneric},
		{"Deloitte", "https://careers.deloitte.ca/search/?q=devops&location=Halifax", scrapeGeneric},
		{"CGI", "https://cgi.njoyn.com/CGI/xweb/XWeb.asp?NTKN=c&clid=21001&Page=JobList&lang=1", scrapeGeneric},
		{"EY", "https://careers.ey.com/ey/search/?q=devops&locationsearch=halifax", scrapeGeneric},
		{"Accenture", "https://www.accenture.com/ca-en/careers/jobsearch?jk=devops&lc=halifax", scrapeGeneric},
		{"IBM", "https://www.ibm.com/ca-en/employment/", scrapeGeneric},
		{"NTT Data", "https://careers-inc.nttdata.com/job-search-results/?keywords=devops&location=Halifax", scrapeGeneric},
		{"Cognizant", "https://careers.cognizant.com/global/en/search-results?keywords=devops&location=Halifax", scrapeGeneric},
		{"Microsoft", "https://careers.microsoft.com/us/en/search-results?keywords=devops&location=Halifax", scrapeGeneric},
		{"Amazon", "https://www.amazon.jobs/en/search?base_query=devops&location=halifax", scrapeGeneric},
		{"Google", "https://careers.google.com/jobs/results/?location=Halifax&q=devops", scrapeGeneric},
		{"Oracle", "https://www.oracle.com/corporate/careers/jobs?keyword=devops&location=halifax", scrapeGeneric},

		/* product & scale-ups */
		{"REDspace", "https://jobs.lever.co/redspace", scrapeGeneric},
		{"Dash Hudson", "https://www.dashhudson.com/careers", scrapeGeneric},
		{"Proposify", "https://www.proposify.com/careers", scrapeGeneric},
		{"Milk Moovement", "https://milkmoovement.com/careers", scrapeGeneric},
		{"MOBIA", "https://www.mobia.io/careers", scrapeGeneric},
		{"CarteNav Solutions", "https://www.cartenav.com/careers/", scrapeGeneric},
		{"GeoSpectrum", "https://geospectrum.ca/careers", scrapeGeneric},
		{"ResMed", "https://resmed.wd3.myworkdayjobs.com/ResMedJobs", scrapeGeneric},

		/* remote-first Canada tech */
		{"CrowdStrike", "https://crowdstrike.wd5.myworkdayjobs.com/CrowdStrikeCareers?locations=6d6b7d53094f01d1c63237f24db0c35d", scrapeGeneric},
		{"Affirm", "https://boards.greenhouse.io/affirm", scrapeGeneric},
		{"Verafin", "https://verafin.com/careers", scrapeGeneric},
		{"Introhive", "https://jobs.lever.co/introhive", scrapeGeneric},

		/* government / defence */
		{"Irving Shipbuilding", "https://www.shipsforcanada.ca/en/home/careers", scrapeGeneric},
		{"Lockheed Martin", "https://www.lockheedmartinjobs.com/search-jobs/DevOps/Halifax/694/1", scrapeGeneric},
	}

	for _, s := range sites {
		wg.Add(1)
		go func(site JobSite) {
			defer wg.Done()
			site.Scraper(site.Name, site.URL)
		}(s)
	}

	wg.Wait()
	fmt.Println("✅ All scrapers finished.")
}

/*─────────────────────────  SPECIFIC SCRAPER: RBC  ─────────────────────────*/

func scrapeRBC(name, url string) {
	fmt.Printf("🔍 Scraping %s...\n", name)
	c := colly.NewCollector(colly.UserAgent(userAgent))

	c.OnHTML("li.job-result", func(e *colly.HTMLElement) {
		title := e.ChildText("h3.job-title")
		location := e.ChildText(".job-location")
		link := e.ChildAttr("a", "href")
		full := "https://jobs.rbc.com" + link
		if containsKeywords(title) && locationContainsHalifax(location) {
			notify(name, title, location, full)
		}
	})

	if err := c.Visit(url); err != nil {
		log.Printf("❌ Error visiting %s: %v", url, err)
	}
}

/*─────────────────────────  GENERIC SCRAPER  ─────────────────────────*/

func scrapeGeneric(name, url string) {
	fmt.Printf("🔍 Scraping %s (generic)...\n", name)
	c := colly.NewCollector(colly.UserAgent(userAgent))

	c.OnHTML("body", func(e *colly.HTMLElement) {
		if containsKeywords(e.Text) {
			notify(name, "Possible cloud/DevOps opening", "", url)
		}
	})

	if err := c.Visit(url); err != nil {
		log.Printf("❌ Error visiting %s: %v", url, err)
	}
}

/*─────────────────────────  NOTIFY & DEDUP  ─────────────────────────*/

func notify(company, title, loc, link string) {
	key := makeHash(company, title, link)
	if !shouldSend(key) {
		return
	}
	newAlerts = true

	msg := fmt.Sprintf("✅ %s: %s %s→ %s", company, title, loc, link)
	fmt.Println(msg)
	sendSlack(msg)
}

/* history helpers */

func shouldSend(h string) bool {
	historyMtx.Lock()
	defer historyMtx.Unlock()

	if ts, ok := history[h]; ok && time.Since(time.Unix(ts, 0)) < dedupWindow {
		return false
	}
	history[h] = time.Now().Unix()
	_ = os.WriteFile(historyFile, mustJSON(history), 0644)
	return true
}

func makeHash(parts ...string) string {
	h := sha1.New()
	for _, p := range parts {
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func mustJSON(v any) []byte {
	b, _ := json.MarshalIndent(v, "", "  ")
	return b
}

/*─────────────────────────  SLACK  ─────────────────────────*/

func sendSlack(text string) {
	payload := fmt.Sprintf(`{"channel":"%s","text":"%s"}`, channelID, text)
	req, _ := http.NewRequest("POST", "https://slack.com/api/chat.postMessage", bytes.NewBuffer([]byte(payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+slackToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("Slack error: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("Slack HTTP %d – %s", resp.StatusCode, body)
	}
}

/*─────────────────────────  UTILS  ─────────────────────────*/

func containsKeywords(text string) bool {
	kws := []string{"devops", "cloud", "platform", "sre", "terraform", "azure", "kubernetes"}
	text = strings.ToLower(text)
	for _, k := range kws {
		if strings.Contains(text, k) {
			return true
		}
	}
	return false
}

func locationContainsHalifax(loc string) bool {
	return strings.Contains(strings.ToLower(loc), "halifax")
}