package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"math/rand/v2"
	"os"
	"strings"
	"time"

	"github.com/codingsandmore/pumpfun-portal/portal"
	"github.com/fatih/color"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/patrickmn/go-cache"
	"golang.org/x/time/rate"
	"trader.fun/config"
	"trader.fun/indicator"
	"trader.fun/indicator/dataset"
	"trader.fun/pumpfun"
)

var (
	cfg       = config.LoadConfig()
	rpcClient = rpc.NewWithCustomRPCClient(rpc.NewWithLimiter(
		cfg.RPCEndpoint,
		rate.Every(time.Second*10), // time frame
		35,                         // limit of requests per time frame
	))
)

func main() {
	//capture_dataset()
	//balance_dataset()
	//virtual_trader()
}

func balance_dataset() {
	fileData, err := ioutil.ReadFile("dataset.txt")
	if err != nil {
		panic(err)
	}
	goodDelim := []byte("=>1")
	goodCoin := bytes.Count(fileData, goodDelim)
	maxCollect := int(float64(goodCoin) * 3)

	toBC := maxCollect - goodCoin
	ncollected := 0
	var collected []string

	for _, lineBytes := range bytes.Split(fileData, []byte("\r\n")) {
		if bytes.Contains(lineBytes, goodDelim) {
			collected = append(collected, string(lineBytes))
		} else if ncollected < toBC {
			collected = append(collected, string(lineBytes))
			ncollected++
		}
	}

	// Open or create the file
	file, err := os.Create("balanced_dataset.txt")
	if err != nil {
		fmt.Println("Error creating file:", err)
		return
	}
	defer file.Close()

	rand.Shuffle(len(collected), func(i, j int) {
		collected[i], collected[j] = collected[j], collected[i]
	})

	// Join the string array with newline characters and write to file
	content := strings.Join(collected, "\r\n")
	_, err = file.WriteString(content)
	if err != nil {
		fmt.Println("Error writing to file:", err)
		return
	}

	fmt.Println("found", goodCoin, "good coins")
}

func virtual_trader() {
	var pf *pumpfun.Pumpfun
	var tradeChan = make(chan *pumpfun.Coin, 1)
	var trading = false

	ch := cache.New(1*time.Minute, 1*time.Minute)

	discoverTrade := func(p *portal.NewTradeResponse) {
		if len(p.BondingCurveKey) == 0 || len(p.Mint) == 0 || p.MarketCapSol == 0 || pf == nil {
			return
		}

		_, found := ch.Get(p.Mint)
		if found {
			return
		}
		ch.Set(p.Mint, true, cache.DefaultExpiration)

		coin := &pumpfun.Coin{
			MintAddr:          solana.MPK(p.Mint),
			TokenBondingCurve: solana.MPK(p.BondingCurveKey),
			MarketCap:         p.MarketCapSol,
		}

		if indicator.ShouldBuy(coin) && !trading {
			tradeChan <- coin
		}
	}

	red := color.New(color.FgRed).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	blue := color.New(color.FgBlue).SprintFunc()
	pf = pumpfun.NewPumpFun(rpcClient, discoverTrade)
	var solBalance = 1.

	fmt.Println(blue(fmt.Sprintf("STARTING SOL BALANCE: %.2f", solBalance)))
	for {
		coin := <-tradeChan
		trading = true
		coinPrice := coin.Price()
		fmt.Println(blue(fmt.Sprintf("Now trading coin %s with start price %.2f and mc %.2f", coin.MintAddr.String(), coinPrice, coin.MarketCap)))
		time.Sleep(3 * time.Second)
		endPrice := coin.Price()
		pc := percentageChange(coinPrice, endPrice)
		solBalance = solBalance * (1 + (pc / 100))
		if pc > 0 {
			fmt.Println(green(fmt.Sprintf("PROFITTED! Coin %s was profitable by %.2f %.2f", coin.MintAddr.String(), pc, solBalance)))
		} else {
			fmt.Println(red(fmt.Sprintf("YOU LOSS! Coin %s was unprofitable by %.2f %.2f", coin.MintAddr.String(), pc, solBalance)))
		}
	}
}

func capture_dataset() {
	var ds *dataset.Dataset
	discoverTrade := func(p *portal.NewTradeResponse) {
		if ds == nil || !strings.HasSuffix(p.Mint, "pump") {
			return
		}
		if len(p.BondingCurveKey) == 0 || len(p.Mint) == 0 {
			return
		}
		go func() {
			if err := ds.Capture(&pumpfun.Coin{
				MintAddr:          solana.MPK(p.Mint),
				TokenBondingCurve: solana.MPK(p.BondingCurveKey),
				MarketCap:         p.MarketCapSol,
			}); err == nil {
				fmt.Println("Captured data:", ds.Captured)
			}
		}()
	}
	pf := pumpfun.NewPumpFun(rpcClient, discoverTrade)
	ds = dataset.New(pf)

	for ds.Captured < 10_000 {
		time.Sleep(1 * time.Second)
	}
}

func percentageChange(oldValue, newValue float64) float64 {

	return ((newValue - oldValue) / oldValue) * 100
}
