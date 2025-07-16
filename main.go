package main

import (
    "bytes"
    "fmt"
    "io"
    "log"
    "net/http"
    "os"
    "strings"
    "sync"

    "github.com/gocolly/colly"
)

type JobSite struct {
    Name    string
    URL     string
    Scraper func(name, url string)
}

var (
    slackToken = os.Getenv("SLACK_BOT_TOKEN")  // xoxb-...
    channelID  = os.Getenv("SLACK_CHANNEL_ID") // Cxxxxxxxx
)

func init() {
    if slackToken == "" || channelID == "" {
        log.Fatal("SLACK_BOT_TOKEN or SLACK_CHANNEL_ID not set")
    }
}

func main() {
    scrapeAll()
}

func scrapeAll() {
    var wg sync.WaitGroup

    // ――― Master list of Halifax-friendly employers (consulting, product, startup, fintech) ―――
    sites := []JobSite{
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

        // ――― Product & scale-ups ―――
        {"REDspace", "https://jobs.lever.co/redspace", scrapeGeneric},
        {"Dash Hudson", "https://www.dashhudson.com/careers", scrapeGeneric},
        {"Proposify", "https://www.proposify.com/careers", scrapeGeneric},
        {"Milk Moovement", "https://milkmoovement.com/careers", scrapeGeneric},
        {"MOBIA", "https://www.mobia.io/careers", scrapeGeneric},
        {"CarteNav Solutions", "https://www.cartenav.com/careers/", scrapeGeneric},
        {"GeoSpectrum", "https://geospectrum.ca/careers", scrapeGeneric},
        {"ResMed", "https://resmed.wd3.myworkdayjobs.com/ResMedJobs", scrapeGeneric},

        // ――― Remote-first Canadian tech (Atlantic friendly) ―――
        {"CrowdStrike", "https://crowdstrike.wd5.myworkdayjobs.com/CrowdStrikeCareers?locations=6d6b7d53094f01d1c63237f24db0c35d", scrapeGeneric},
        {"Affirm", "https://boards.greenhouse.io/affirm", scrapeGeneric},
        {"Verafin", "https://verafin.com/careers", scrapeGeneric},
        {"Introhive", "https://jobs.lever.co/introhive", scrapeGeneric},

        // ――― Government & defence ―――
        {"Irving Shipbuilding", "https://www.shipsforcanada.ca/en/home/careers", scrapeGeneric},
        {"Lockheed Martin", "https://www.lockheedmartinjobs.com/search-jobs/DevOps/Halifax/694/1", scrapeGeneric},
    }

    // run in parallel
    for _, site := range sites {
        wg.Add(1)
        go func(site JobSite) {
            defer wg.Done()
            site.Scraper(site.Name, site.URL)
        }(site)
    }

    wg.Wait()
    fmt.Println("✅ All scrapers finished.")
}

// ───────────────────────── Specific scraper for RBC (Workday adds easy selectors) ─────────────────────────
func scrapeRBC(name, url string) {
    fmt.Printf("🔍 Scraping %s...\n", name)
    c := colly.NewCollector(colly.UserAgent("Mozilla/5.0"))

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

// ───────────────────────── Generic fallback scraper ─────────────────────────
func scrapeGeneric(name, url string) {
    fmt.Printf("🔍 Scraping %s (generic)...\n", name)
    c := colly.NewCollector(colly.UserAgent("Mozilla/5.0"))

    c.OnHTML("body", func(e *colly.HTMLElement) {
        text := strings.ToLower(e.Text)
        if strings.Contains(text, "devops") || strings.Contains(text, "cloud") || strings.Contains(text, "platform") {
            notify(name, "Possible cloud/DevOps opening", "", url)
        }
    })

    if err := c.Visit(url); err != nil {
        log.Printf("❌ Error visiting %s: %v", url, err)
    }
}

// ───────────────────────── Helper functions ─────────────────────────
func notify(company, title, loc, link string) {
    msg := fmt.Sprintf("✅ %s: %s %s→ %s", company, title, loc, link)
    fmt.Println(msg)
    f, _ := os.OpenFile("matches.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    defer f.Close()
    fmt.Fprintln(f, msg)
    sendSlack(msg)
}

func sendSlack(message string) {
    payload := fmt.Sprintf(`{"channel":"%s","text":"%s"}`, channelID, message)
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

func containsKeywords(t string) bool {
    kws := []string{"devops", "cloud", "platform", "sre", "terraform", "azure"}
    t = strings.ToLower(t)
    for _, k := range kws {
        if strings.Contains(t, k) {
            return true
        }
    }
    return false
}

func locationContainsHalifax(loc string) bool {
    return strings.Contains(strings.ToLower(loc), "halifax")
}
