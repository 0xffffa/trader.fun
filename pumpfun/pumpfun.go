package pumpfun

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/codingsandmore/pumpfun-portal/portal"
	"github.com/codingsandmore/pumpfun-portal/portal/server"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/rs/zerolog"
)

type Pumpfun struct {
	Client       *rpc.Client
	TrendingWord string
	PumpPortal   *server.PortalServer
}

// check how far based on marketcap in sol
func (pf *Pumpfun) IsMemeTrending(name string) bool {
	return strings.Contains(name, pf.TrendingWord)
}

func (pf *Pumpfun) UpdateTrendingWord() {
	for range time.NewTicker(1 * time.Minute).C {
		trending := pf.FetchTrendingWord()

		if len(trending) == 0 {
			continue
		}

		pf.TrendingWord = trending
	}

}

func (pf *Pumpfun) FetchTrendingWord() (trending string) {
	options := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("blink-settings", "imagesEnabled=false"),        // block all images
		chromedp.Flag("headless", true),                               // Run in headless mode (no UI)
		chromedp.Flag("no-sandbox", true),                             // Run without a sandbox (required for some environments)
		chromedp.Flag("disable-gpu", true),                            // Disable GPU hardware acceleration
		chromedp.Flag("disable-software-rasterizer", true),            // Disable software rasterizer
		chromedp.Flag("disable-extensions", true),                     // Disable extensions
		chromedp.Flag("disable-background-timer-throttling", true),    // Disable background throttling
		chromedp.Flag("disable-backgrounding-occluded-windows", true), // Disable occluded windows
		chromedp.Flag("mute-audio", true),                             // Mute audio
		chromedp.Flag("disable-notifications", true),                  // Disable notifications
		chromedp.NoDefaultBrowserCheck,
		chromedp.NoFirstRun,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	allocatorCtx, cancel := chromedp.NewExecAllocator(ctx, options...)
	defer cancel()

	ctx, cancel = chromedp.NewContext(allocatorCtx)
	defer cancel()

	var htmlContent string
	err := chromedp.Run(ctx,
		network.Enable(),
		network.SetBlockedURLs([]string{"*.png", "*.jpg", "*.jpeg", "*.gif", "*.svg", "*.css", "*.woff", "*.woff2"}),
		chromedp.Navigate("https://pump.fun/board"),
		chromedp.WaitVisible("div", chromedp.ByQuery),
		chromedp.OuterHTML("html", &htmlContent),
	)
	if err != nil {
		return
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return
	}

	doc.Find("div").Each(func(i int, s *goquery.Selection) {
		if s.Text() == "trending:" {
			nextDiv := s.Next()
			button := nextDiv.Find("button").First()
			trending = button.Text()

			reg, _ := regexp.Compile("[^a-zA-Z0-9]+")

			trending = reg.ReplaceAllString(trending, "")
			trending = strings.TrimLeft(trending, " ")
			trending = strings.TrimSpace(trending)
			trending = strings.ReplaceAll(trending, "🔥", "")
		}
	})

	return
}

func NewPumpFun(rpcClient *rpc.Client, discoverTrade func(p *portal.NewTradeResponse)) *Pumpfun {
	pf := &Pumpfun{
		Client: rpcClient,
	}

	/*if pumpPortal {

	}*/

	zerolog.SetGlobalLevel(zerolog.Disabled)
	pf.PumpPortal = server.NewPortalServer()
	discoverPair := func(p *portal.NewPairResponse) {}

	go pf.PumpPortal.Discover(discoverPair, discoverTrade)
	go pf.UpdateTrendingWord()

	return pf
}
