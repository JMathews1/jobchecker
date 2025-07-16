package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"net/http"
	"bytes"

	"github.com/gocolly/colly"
)

type JobSite struct {
	Name    string
	URL     string
	Scraper func(name, url string)
}

// Set your Slack webhook URL here
const slackWebhookURL = "https://hooks.slack.com/services/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"

func main() {
	scrapeAll()
}

func scrapeAll() {
	var wg sync.WaitGroup
	fmt.Println(slackWebhookURL)
	sites := []JobSite{
		{"RBC", "https://jobs.rbc.com/ca/en/search-results?keywords=devops&location=Halifax", scrapeRBC},
		{"EY", "https://careers.ey.com/ey/search/?q=devops&locationsearch=halifax", scrapeGeneric},
		{"CGI", "https://cgi.njoyn.com/CGI/xweb/XWeb.asp?NTKN=c&clid=21001&Page=JobList&lang=1", scrapeGeneric},
		{"ResMed", "https://resmed.wd3.myworkdayjobs.com/en-US/ResMedJobs", scrapeGeneric},
		{"Cognizant", "https://careers.cognizant.com/global/en/search-results", scrapeGeneric},
		{"NTT Data", "https://careers-inc.nttdata.com/job-search-results/", scrapeGeneric},
		{"ZayZoon", "https://zayzoon.com/careers", scrapeGeneric},
		{"Exposant 3", "https://exposant3.com/en/careers", scrapeGeneric},
		{"Deloitte", "https://careers.deloitte.ca/search/?q=devops", scrapeGeneric},
		{"Accenture", "https://www.accenture.com/ca-en/careers/jobsearch", scrapeGeneric},
		{"KPMG", "https://home.kpmg/ca/en/home/careers.html", scrapeGeneric},
		{"IBM", "https://www.ibm.com/ca-en/employment/", scrapeGeneric},
		{"Microsoft", "https://careers.microsoft.com/us/en/search-results", scrapeGeneric},
		{"Amazon", "https://www.amazon.jobs/en/locations/halifax-canada", scrapeGeneric},
		{"Google", "https://careers.google.com/jobs/results/?location=Halifax", scrapeGeneric},
		{"Oracle", "https://www.oracle.com/corporate/careers/", scrapeGeneric},
		{"TD Bank", "https://jobs.td.com/en-CA/search-results/", scrapeGeneric},
		{"Scotiabank", "https://jobs.scotiabank.com/search/?q=devops", scrapeGeneric},
		{"Sun Life Financial", "https://www.sunlife.ca/en/careers/", scrapeGeneric},
		{"Intone Networks", "https://www.intonenetworks.com/careers/", scrapeGeneric},
		{"DataAnnotation", "https://dataannotation.tech/careers", scrapeGeneric},
		{"Atlantis IT Group", "https://www.atlantisitgroup.com/careers", scrapeGeneric},
		{"Tiger Analytics", "https://www.tigeranalytics.com/careers/", scrapeGeneric},
		{"BeyondTrust", "https://www.beyondtrust.com/company/careers", scrapeGeneric},
		{"Lockheed Martin", "https://www.lockheedmartinjobs.com/", scrapeGeneric},
		{"Cabot Technology Solutions", "https://www.cabotsolutions.com/careers", scrapeGeneric},
		{"Dash Hudson", "https://www.dashhudson.com/careers", scrapeGeneric},
		{"Proposify", "https://www.proposify.com/careers", scrapeGeneric},
		{"Blue", "https://bluecreative.ca/careers/", scrapeGeneric},
		{"Equals6", "https://www.equals6.com/about-us/careers/", scrapeGeneric},
		{"Kinduct Technologies", "https://www.kinduct.com/about/careers/", scrapeGeneric},
		{"CarteNav Solutions", "https://www.cartenav.com/careers/", scrapeGeneric},
		{"Eastlink", "https://www.eastlink.ca/about/careers", scrapeGeneric},
		{"Trampoline Branding", "https://trampolinebranding.com/careers/", scrapeGeneric},
		{"Irving Shipbuilding", "https://www.shipsforcanada.ca/en/home/careers", scrapeGeneric},
	}

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

func scrapeRBC(name, url string) {
	fmt.Printf("🔍 Scraping %s...\n", name)
	c := colly.NewCollector(
		colly.UserAgent("Mozilla/5.0"),
	)

	c.OnHTML("li.job-result", func(e *colly.HTMLElement) {
		title := e.ChildText("h3.job-title")
		location := e.ChildText(".job-location")
		link := e.ChildAttr("a", "href")
		fullLink := "https://jobs.rbc.com" + link

		if containsKeywords(title) && locationContainsHalifax(location) {
			msg := fmt.Sprintf("✅ %s: %s [%s] → %s", name, title, location, fullLink)
			fmt.Println(msg)

			f, _ := os.OpenFile("matches.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			defer f.Close()
			fmt.Fprintln(f, msg)

			sendSlackNotification(msg)
		}
	})

	if err := c.Visit(url); err != nil {
		log.Printf("❌ Error visiting %s: %v\n", url, err)
	}
}

func scrapeGeneric(name, url string) {
	fmt.Printf("🔍 Scraping %s (generic)...\n", name)
	c := colly.NewCollector(
		colly.UserAgent("Mozilla/5.0"),
	)

	c.OnHTML("body", func(e *colly.HTMLElement) {
		text := strings.ToLower(e.Text)
		if strings.Contains(text, "devops") || strings.Contains(text, "cloud") || strings.Contains(text, "platform") {
			msg := fmt.Sprintf("⚠️  Possible match found on %s → %s", name, url)
			fmt.Println(msg)

			f, _ := os.OpenFile("matches.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			defer f.Close()
			fmt.Fprintln(f, msg)

			sendSlackNotification(msg)
		}
	})

	if err := c.Visit(url); err != nil {
		log.Printf("❌ Error visiting %s: %v\n", url, err)
	}
}

func sendSlackNotification(message string) {
	jsonStr := fmt.Sprintf(`{"text":"%s"}`, message)
	req, err := http.NewRequest("POST", slackWebhookURL, bytes.NewBuffer([]byte(jsonStr)))
	if err != nil {
		log.Printf("Slack error: %v\n", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Slack error: %v\n", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Printf("Slack error: received status code %d\n", resp.StatusCode)
	}
}

func containsKeywords(title string) bool {
	keywords := []string{"devops", "cloud", "platform", "software", "sre"}
	for _, k := range keywords {
		if strings.Contains(strings.ToLower(title), k) {
			return true
		}
	}
	return false
}

func locationContainsHalifax(loc string) bool {
	return strings.Contains(strings.ToLower(loc), "halifax")
}