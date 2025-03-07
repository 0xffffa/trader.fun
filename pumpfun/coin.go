package pumpfun

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bogdanfinn/tls-client/profiles"
	"github.com/cdipaolo/sentiment"
	"github.com/corpix/uarand"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"golang.org/x/time/rate"

	http "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
)

var (
	languageModel, _ = sentiment.Restore()
	maxSol           = 100_000
	miniMaxSol       = 1000
	maxMs            = 10_000
	maxTx            = 10_000
	netClient        = coinClient()
	rpcClient        = rpc.NewWithCustomRPCClient(rpc.NewWithLimiter(
		rpc.MainNetBeta_RPC,
		rate.Every(time.Second*10), // time frame
		35,                         // limit of requests per time frame
	))
)

type PumpWallet string

type Coin struct {
	MintAddr               solana.PublicKey
	TokenBondingCurve      solana.PublicKey
	AssociatedBondingCurve solana.PublicKey
	MarketCap              float64
}

type Candle struct {
	High, Low, Open, Close float64
}

type Comment struct {
	CommentId string
	Owner     PumpWallet
	Msg       string
}

func (c *Coin) Compile() (data []float64) {
	var (
		metadata map[string]interface{}
		candles  []Candle
		comments []*Comment
	)

	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); metadata = c.Metadata() }()
	go func() { defer wg.Done(); candles = c.Candles() }()
	go func() { defer wg.Done(); comments = c.Comments() }()
	wg.Wait()

	var (
		dexPaid               bool
		rugChance             float64
		raydiumProgress       float64
		kothProgress          float64
		commentCount          float64
		commentPositivity     float64
		hasTwitter            bool
		hasTelegram           bool
		hasWebsite            bool
		rsi, mar, sd          float64
		memeTrending          bool
		fibIndicator          bool
		isNew                 bool
		volatility            float64
		tradeCount, volume    float64
		buyVolume, sellVolume float64
		lastBuy, lastSell     float64
		buyers, sellers       float64
		snipers, holders      float64
		candlesMP, candlesEMA []float64
		candlesData           [][]float64
	)

	wg.Add(3)
	go func() { defer wg.Done(); dexPaid = c.IsDexPaid() }()
	go func() { defer wg.Done(); rugChance = c.RugChance() }()
	go func() { defer wg.Done(); kothProgress = c.GetKothPercent() }()

	wg.Add(1)
	go func() {
		defer wg.Done()
		hasWebsite = metadata["hasWebsite"].(bool)
		hasTelegram = metadata["hasTelegram"].(bool)
		hasTwitter = metadata["hasTwitter"].(bool)
		isNew = metadata["isNew"].(bool)
		commentCount = float64(len(comments))
		if commentCount > float64(maxTx) {
			commentCount = float64(maxTx)
		}
		commentCount /= float64(maxTx)
		commentPositivity = c.CommentPositivity(comments)

		rsi = c.RSI(candles)
		mar = c.MAR(candles)
		sd = c.StandardDeviation(candles)
		memeTrending = (&Pumpfun{}).IsMemeTrending(metadata["name"].(string))
		fibIndicator = c.FibIndicator(candles, rsi)
		volatility = c.Volatility(candles)
		tradeCount = float64(c.Trades())
		if tradeCount > float64(maxTx) {
			tradeCount = float64(maxTx)
		}
		tradeCount /= float64(maxTx)
		candlesMP = c.CandlesMP(candles)
		candlesEMA = c.EMA(candles)
		for _, candle := range candles {
			candlesData = append(candlesData, []float64{candle.Close, candle.High, candle.Low, candle.High})
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		holders, volume, lastBuy, lastSell, buyers, sellers,
			buyVolume, sellVolume, snipers, raydiumProgress = c.MarketInfo()
	}()

	wg.Wait()

	marketCap := c.MarketCap / float64(maxSol)
	if marketCap > 1 {
		marketCap = 1
	}

	rsi = c.convertIfNana(rsi)
	mar = c.convertIfNana(mar)
	sd = c.convertIfNana(sd)
	volatility = c.convertIfNana(volatility)

	for i, f := range candlesMP {
		candlesMP[i] = c.convertIfNana(f)
	}

	for i, f := range candlesEMA {
		candlesEMA[i] = c.convertIfNana(f)
	}

	for i, f := range candlesData {
		for a, b := range f {
			candlesData[i][a] = c.convertIfNana(b)
		}
	}

	data = append(data, c.boolToFloat(dexPaid), rugChance, raydiumProgress) // volume check needs fixing
	data = append(data, kothProgress, commentCount, commentPositivity)
	data = append(data, c.boolToFloat(hasTwitter), c.boolToFloat(hasWebsite), c.boolToFloat(hasTelegram))
	data = append(data, rsi, mar, sd)
	data = append(data, c.boolToFloat(memeTrending), c.boolToFloat(fibIndicator), c.boolToFloat(isNew))
	data = append(data, volatility, tradeCount, buyVolume)
	data = append(data, lastBuy, lastSell, buyers)
	data = append(data, sellers, snipers, holders)
	data = append(data, volume, marketCap, sellVolume)
	data = append(data, candlesMP...)
	data = append(data, candlesEMA...)
	data = append(data, candlesData[0]...)
	data = append(data, candlesData[1]...)
	data = append(data, candlesData[2]...)

	return
}

func (c *Coin) IsDexPaid() bool {
	req, err := http.NewRequest("GET", "https://api.dexscreener.com/orders/v1/solana/"+c.MintAddr.String(), nil)
	if err != nil {
		return false
	}

	req.Header = http.Header{
		"User-Agent": {uarand.GetRandom()},
	}

	resp, err := netClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return false
	}
	var dexData []interface{}
	if err := json.Unmarshal(body, &dexData); err != nil {
		return false
	}

	return len(dexData) > 0
}

func (c *Coin) RugChance() float64 {
	req, err := http.NewRequest("GET", "https://api.rugcheck.xyz/v1/tokens/"+c.MintAddr.String()+"/report/summary", nil)
	if err != nil {
		return 0
	}

	req.Header = http.Header{
		"User-Agent": {uarand.GetRandom()},
	}

	resp, err := netClient.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return 0
	}

	var bodyMap = make(map[string]interface{})
	if err := json.Unmarshal(body, &bodyMap); err != nil {
		return 0
	}

	minRisk := 1.0
	maxRisk := 10000.0
	riskScore, ok := bodyMap["score"]
	if !ok {
		return 0
	}
	if riskScore.(float64) < minRisk {
		riskScore = minRisk
	} else if riskScore.(float64) > maxRisk {
		riskScore = maxRisk
	}

	return (riskScore.(float64) - minRisk) / (maxRisk - minRisk)
}

func (c *Coin) Comments() []*Comment {
	var comments []*Comment

	req, err := http.NewRequest("GET", "https://frontend-api-v2.pump.fun/replies/"+c.MintAddr.String()+"?limit=500&offset=0&user=string&reverseOrder=false", nil)
	if err != nil {
		return comments
	}

	req.Header = http.Header{
		"User-Agent": {uarand.GetRandom()},
	}

	resp, err := netClient.Do(req)
	if err != nil {
		return comments
	}
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)

	type PfComments struct {
		Replies []map[string]interface{} `json:"replies"`
		HasMore bool                     `json:"hasMore"`
		Offset  int                      `json:"offset"`
	}

	var pfc PfComments
	if err := json.Unmarshal(body, &pfc); err != nil {
		return comments
	}

	for _, reply := range pfc.Replies {
		user := reply["user"].(string)
		id := int(reply["id"].(float64))
		msg := reply["text"].(string)
		comments = append(comments, &Comment{
			Owner:     PumpWallet(user),
			CommentId: fmt.Sprintf("%d", id),
			Msg:       msg,
		})
	}

	return comments

}

// how many holders/volume/last buy/ last sell/buys/sells,buyVolm,SellVolm,sniper_count, progress to raydium
func (c *Coin) MarketInfo() (float64, float64, float64, float64, float64, float64, float64, float64, float64, float64) {
	req, err := http.NewRequest("GET", "https://advanced-api-v2.pump.fun/coins/metadata-and-trades/"+c.MintAddr.String(), nil)
	if err != nil {
		return 0, 0, 0, 0, 0, 0, 0, 0, 0, 0
	}

	req.Header = http.Header{
		"User-Agent": {uarand.GetRandom()},
	}

	resp, err := netClient.Do(req)
	if err != nil {
		return 0, 0, 0, 0, 0, 0, 0, 0, 0, 0
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return 0, 0, 0, 0, 0, 0, 0, 0, 0, 0
	}

	var marketInfo = make(map[string]interface{})
	if err := json.Unmarshal(body, &marketInfo); err != nil {
		return 0, 0, 0, 0, 0, 0, 0, 0, 0, 0
	}

	if marketInfo["coin"] == nil || marketInfo["trades"] == nil {
		return 0, 0, 0, 0, 0, 0, 0, 0, 0, 0
	}

	coin := marketInfo["coin"].(map[string]interface{})
	tradesData := marketInfo["trades"].(map[string]interface{})

	if len(coin) == 0 {
		return 0, 0, 0, 0, 0, 0, 0, 0, 0, 0
	}

	var sniperCount int
	sc, found := coin["sniper_count"].(string)
	if found {
		sniperCount, _ = strconv.Atoi(sc)
	}

	var holders int
	h, found := coin["num_holders"].(string)
	if found {
		holders, _ = strconv.Atoi(h)
	}

	volume, _ := strconv.ParseFloat(coin["volume"].(string), 64)
	marketCap, _ := strconv.ParseFloat(coin["marketcap"].(string), 64)
	progress, _ := strconv.ParseFloat(coin["progress"].(string), 64)

	buyVolume := 0.
	sellVolume := 0.
	lastBuy := 0.
	lastSell := 0.
	recentBuys := 0.
	recentSells := 0.

	trades, ok := tradesData[c.MintAddr.String()].([]interface{})
	if !ok {
		return float64(holders) / float64(maxTx), float64(volume) / float64(maxSol), lastBuy, lastSell, recentBuys, recentSells, buyVolume, sellVolume, float64(sniperCount) / float64(maxTx), progress / 100.
	}

	for _, tradeInf := range trades {
		trade := tradeInf.(map[string]interface{})
		solAmount := trade["sol_amount"].(float64)
		timestamp := int(trade["timestamp"].(float64))
		if trade["is_buy"].(bool) {
			recentBuys++
			if float64(timestamp) > float64(lastBuy) || lastBuy == 0 {
				lastBuy = float64(timestamp)
			}
			buyVolume += solAmount
		} else {
			if float64(timestamp) > float64(lastSell) || lastSell == 0 {
				lastSell = float64(timestamp)
			}
			sellVolume += solAmount
			recentSells++
		}
	}

	buyVolume = c.lamportsToSol(uint64(buyVolume))
	if buyVolume > float64(miniMaxSol) {
		buyVolume = float64(miniMaxSol)
	} else {
		buyVolume /= float64(miniMaxSol)
	}

	sellVolume = c.lamportsToSol(uint64(sellVolume))
	if sellVolume > float64(miniMaxSol) {
		sellVolume = float64(miniMaxSol)
	} else {
		sellVolume /= float64(miniMaxSol)
	}

	recentBuys /= float64(maxTx)
	recentSells /= float64(maxTx)

	lastSell = float64(time.Since(time.Unix(0, int64(lastSell)*int64(time.Millisecond))).Milliseconds())
	lastBuy = float64(time.Since(time.Unix(0, int64(lastBuy)*int64(time.Millisecond))).Milliseconds())

	if lastSell > float64(maxMs) {
		lastSell = float64(maxMs)
	}

	if lastBuy > float64(maxMs) {
		lastBuy = float64(maxMs)
	}

	lastSell /= float64(maxMs)
	lastBuy /= float64(maxMs)

	if marketCap != 0 {
		c.MarketCap = marketCap
	}

	if sniperCount > maxTx {
		sniperCount = maxTx
	}

	if holders > maxTx {
		holders = maxTx
	}

	if volume > float64(maxSol) {
		volume = float64(maxSol)
	}

	return float64(holders) / float64(maxTx), float64(volume) / float64(maxSol), lastBuy, lastSell, recentBuys, recentSells, buyVolume, sellVolume, float64(sniperCount) / float64(maxTx), progress / 100.
}

func (c *Coin) GetKothPercent() float64 {
	req, err := http.NewRequest("GET", "https://frontend-api-v2.pump.fun/coins/king-of-the-hill?includeNsfw=true", nil)
	if err != nil {
		return 0
	}

	req.Header = http.Header{
		"User-Agent": {uarand.GetRandom()},
	}

	resp, err := netClient.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return 0
	}

	var jsonMap map[string]interface{}
	if err := json.Unmarshal(body, &jsonMap); err != nil {
		return 0
	}

	kothMC, ok := jsonMap["market_cap"].(float64)
	if !ok {
		return 0
	}

	if kothMC < c.MarketCap {
		return 0
	}

	return c.MarketCap / kothMC
}

func (c *Coin) Metadata() map[string]interface{} {
	var metadataMap = make(map[string]interface{})
	metadataMap["name"] = ""
	metadataMap["symbol"] = ""
	metadataMap["hasTwitter"] = false
	metadataMap["hasWebsite"] = false
	metadataMap["hasTelegram"] = false
	metadataMap["isNew"] = false

	req, err := http.NewRequest("GET", "https://frontend-api-v2.pump.fun/coins/"+c.MintAddr.String()+"?sync=false", nil)
	if err != nil {
		return metadataMap
	}

	req.Header = http.Header{
		"User-Agent": {uarand.GetRandom()},
	}

	resp, err := netClient.Do(req)
	if err != nil {
		return metadataMap
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return metadataMap
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return metadataMap
	}

	var metadata = make(map[string]interface{})
	if err := json.Unmarshal(body, &metadata); err != nil {
		return metadataMap
	}

	metadataMap["isNew"] = time.Since(time.Unix(0, int64(metadata["created_timestamp"].(float64))*int64(time.Millisecond))) < 10*time.Minute
	metadataMap["name"] = metadata["name"]
	metadataMap["symbol"] = metadata["symbol"]
	metadataMap["hasTwitter"] = metadata["twitter"] != nil
	metadataMap["hasWebsite"] = metadata["website"] != nil
	metadataMap["hasTelegram"] = metadata["telegram"] != nil

	c.AssociatedBondingCurve = solana.MPK(metadata["associated_bonding_curve"].(string))

	return metadataMap
}

func (c *Coin) CommentPositivity(comments []*Comment) float64 {
	var positivity = 0.5

	var badWords = strings.Split("scam,rug,rugpull", ",")
	var goodWords = strings.Split("bump,pump,bumpbot,moon", ",")

	for _, comment := range comments {
		for _, bw := range badWords {
			if strings.Contains(comment.Msg, bw) {
				return 0
			}
		}

		for _, gw := range goodWords {
			if strings.Contains(comment.Msg, gw) {
				return 1
			}
		}

		if languageModel == nil {
			continue
		}

		if languageModel.SentimentAnalysis(comment.Msg, sentiment.English).Score == 1 {
			positivity += 0.5 / float64(len(comments))
		} else {
			positivity -= 0.5 / float64(len(comments))
		}
	}

	if positivity < 0 {
		positivity = 0
	} else if positivity > 1 {
		positivity = 1
	}

	return positivity
}

// heikin ashi candles
func (c *Coin) Candles() []Candle {
	var ret []Candle
	for i := 0; i < 3; i++ {
		ret = append(ret, Candle{})
	}

	req, err := http.NewRequest("GET", "https://frontend-api-v3.pump.fun/candlesticks/"+c.MintAddr.String()+"?offset=0&limit=3&timeframe=1", nil)
	if err != nil {
		return ret
	}

	req.Header = http.Header{
		"User-Agent": {uarand.GetRandom()},
	}

	resp, err := netClient.Do(req)
	if err != nil {
		return ret
	}
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)
	if len(body) == 0 {
		return ret
	}

	type Candles []struct {
		Mint      string  `json:"mint"`
		Timestamp int     `json:"timestamp"`
		Open      float64 `json:"open"`
		High      float64 `json:"high"`
		Low       float64 `json:"low"`
		Close     float64 `json:"close"`
		Volume    int64   `json:"volume"`
		Slot      int     `json:"slot"`
		Is5Min    bool    `json:"is_5_min"`
		Is1Min    bool    `json:"is_1_min"`
	}

	var candles Candles

	if err := json.Unmarshal(body, &candles); err != nil {
		return ret
	}

	countLeadingZeros := func(n float64) int {
		nStr := strconv.FormatFloat(n, 'f', -1, 64)
		zeroCount := 0
		for i := 2; i < len(nStr); i++ { // Skip "0."
			if nStr[i] == '0' {
				zeroCount++
			} else {
				break
			}
		}
		return zeroCount
	}

	adjustNumber := func(n float64) float64 {
		if n >= 1.0 {
			return n // No adjustment needed
		}

		if countLeadingZeros(n) > 5 {
			return n * 100000 // Move it two decimal places
		}

		if countLeadingZeros(n) > 3 {
			return n * 100 // Move it two decimal places
		}

		return n
	}

	var haCandles []Candle
	for i, c := range candles {
		var haCandle Candle
		haCandle.Close = adjustNumber((c.Open + c.High + c.Low + c.Close) / 4)

		if i == 0 {
			haCandle.Open = adjustNumber(c.Open)
		} else {
			haCandle.Open = adjustNumber((haCandles[i-1].Open + haCandles[i-1].Close) / 2)
		}

		haCandle.High = adjustNumber(max(c.High, haCandle.Open, haCandle.Close))
		haCandle.Low = adjustNumber(min(c.Low, haCandle.Open, haCandle.Close))

		haCandles = append(haCandles, haCandle)
	}

	if len(haCandles) < 3 {
		var toAdd []Candle
		for i := 0; i < 3-len(haCandles); i++ {
			toAdd = append(toAdd, Candle{})
		}
		haCandles = append(toAdd, haCandles...)
	}

	return haCandles
}

// 1s%, 2s%, 3s%
func (c *Coin) CandlesMP(candles []Candle) []float64 {
	percentageChange := func(old, new float64) float64 {
		return ((new - old) / old) * 100
	}

	change1s := percentageChange(candles[0].Close, candles[1].Close)
	change2s := percentageChange(candles[0].Close, candles[2].Close)
	change3s := percentageChange(candles[1].Close, candles[2].Close)

	return []float64{change1s, change2s, change3s}
}

func (c *Coin) EMA(candles []Candle) []float64 {
	var emaValues []float64
	period := len(candles)

	// Calculate the multiplier for EMA
	multiplier := 2.0 / (float64(period) + 1)

	// Calculate the EMA for each candle
	ema := candles[0].Close
	for i := 0; i < len(candles); i++ {
		ema = ((candles[i].Close - ema) * multiplier) + ema
		emaValues = append(emaValues, ema)
	}
	return emaValues
}

func (c *Coin) StandardDeviation(candles []Candle) float64 {
	if len(candles) == 0 {
		return 0
	}

	// Calculate the mean (average) of the close prices
	var sum float64
	for _, candle := range candles {
		sum += candle.Close
	}
	mean := sum / float64(len(candles))

	// Calculate the variance
	var varianceSum float64
	for _, candle := range candles {
		diff := candle.Close - mean
		varianceSum += diff * diff
	}
	variance := varianceSum / float64(len(candles))
	sd := math.Sqrt(variance)
	if sd > 1 {
		sd = 1
	}
	if sd < 0 {
		sd = 0
	}
	// Return the square root of the variance (standard deviation)
	return sd
}

func (c *Coin) Volatility(candles []Candle) float64 {
	returns := make([]float64, 2)
	for i := 0; i < 2; i++ {
		returns[i] = (candles[i+1].Close - candles[i].Close) / candles[i].Close
	}

	// Calculate standard deviation of returns
	mean := (returns[0] + returns[1]) / 2
	var sumSquares float64
	for _, r := range returns {
		sumSquares += (r - mean) * (r - mean)
	}

	volatility := math.Sqrt(sumSquares / float64(len(returns)))
	if volatility > 1 {
		volatility = 1
	}
	if volatility < 0 {
		volatility = 0
	}
	return volatility
}

func (c *Coin) RSI(candles []Candle) float64 {
	period := len(candles)

	// Calculate the gains and losses over the period
	var gains, losses float64
	for i := 1; i < period; i++ {
		change := candles[i].Close - candles[i-1].Close
		if change > 0 {
			gains += change
		} else {
			losses -= change
		}
	}

	// Calculate the average gain and loss
	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)

	// Calculate the Relative Strength (RS)
	if avgLoss == 0 {
		return 1
	}
	rs := avgGain / avgLoss

	// Calculate the RSI
	rsi := 100 - (100 / (1 + rs))
	return rsi / 100.
}

func (c *Coin) MAR(candles []Candle) float64 {
	if len(candles) < 2 {
		return 0
	}

	// Calculate the total return
	var totalReturn float64
	for i := 1; i < len(candles); i++ {
		totalReturn += (candles[i].Close - candles[i-1].Close) / candles[i-1].Close
	}

	// Calculate the average return
	averageReturn := totalReturn / float64(len(candles)-1)

	// Calculate the standard deviation of returns
	var varianceSum float64
	for i := 1; i < len(candles); i++ {
		dailyReturn := (candles[i].Close - candles[i-1].Close) / candles[i-1].Close
		diff := dailyReturn - averageReturn
		varianceSum += diff * diff
	}
	variance := varianceSum / float64(len(candles)-1)
	standardDeviation := math.Sqrt(variance)

	// Calculate the MAR ratio (average return / standard deviation of return)
	if standardDeviation == 0 {
		return 0
	}

	mar := averageReturn / standardDeviation
	if mar > 2 {
		mar = 2
	} else if mar < -2 {
		mar = -2
	}

	return c.normalize(mar, -2, 2, 0, 1)
}

func (c *Coin) SMA(candles []Candle) float64 {
	var prices []float64
	for i := 0; i < len(candles); i++ {
		prices = append(prices, candles[i].Close)
	}
	var sum float64
	for _, price := range prices {
		sum += price
	}
	return sum / float64(len(prices))
}

// up or down based on fib
func (c *Coin) FibIndicator(candles []Candle, rsi float64) bool {

	sma := c.SMA(candles)
	high, low := c.GetHighestAndLowestPrice(candles)
	fibonacciLevels := c.FibonacciLevels(high, low)
	lastCandle := candles[len(candles)-1]
	rsi = rsi * 100

	// buy
	if lastCandle.Close < fibonacciLevels[0] && rsi < 30 && lastCandle.Close > sma {
		return true
	}

	// sell
	if lastCandle.Close > fibonacciLevels[3] && rsi > 70 && lastCandle.Close < sma {
		return false
	}

	// else hold (but we dont implement for our example)

	return false
}

func (c *Coin) GetHighestAndLowestPrice(candles []Candle) (float64, float64) {
	// Initialize highest and lowest to extreme values
	highest := math.Inf(-1) // Negative infinity to find maximum
	lowest := math.Inf(1)   // Positive infinity to find minimum

	// Loop through all the candles to find the highest and lowest price
	for _, candle := range candles {
		if candle.High > highest {
			highest = candle.High
		}
		if candle.Low < lowest {
			lowest = candle.Low
		}
	}

	// Return the highest and lowest prices
	return highest, lowest
}

func (c *Coin) FibonacciLevels(high, low float64) []float64 {
	fibRange := high - low
	level23_6 := high - fibRange*0.236
	level38_2 := high - fibRange*0.382
	level50 := high - fibRange*0.5
	level61_8 := high - fibRange*0.618
	level100 := low
	return []float64{level23_6, level38_2, level50, level61_8, level100}
}

func (c *Coin) Trades() int {
	req, err := http.NewRequest("GET", "https://frontend-api-v2.pump.fun/trades/count/"+c.MintAddr.String()+"?minimumSize=0", nil)
	if err != nil {
		return 0
	}

	req.Header = http.Header{
		"User-Agent": {uarand.GetRandom()},
	}

	resp, err := netClient.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)
	if len(body) == 0 {
		return 0
	}

	count, _ := strconv.Atoi(string(body))

	return count
}

func (c *Coin) Price() float64 {
	price, _ := PriceInSolFromBondingCurveAddress(rpcClient, c.TokenBondingCurve.String())

	return price
}

func (c *Coin) lamportsToSol(lamports uint64) float64 {
	// 1 SOL = 1,000,000,000 Lamports
	const lamportsPerSol = 1_000_000_000
	return float64(lamports) / float64(lamportsPerSol)
}

func (c *Coin) normalize(value, minInput, maxInput, minOutput, maxOutput float64) float64 {
	// Normalize value from one range to another
	return ((value-minInput)/(maxInput-minInput))*(maxOutput-minOutput) + minOutput
}

func (c *Coin) boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
func (c *Coin) convertIfNana(f float64) float64 {
	if math.IsNaN(f) {
		return 0
	}
	if math.IsInf(f, 0) {
		return 0
	}
	return f
}

func coinClient() tls_client.HttpClient {
	jar := tls_client.NewCookieJar()

	options := []tls_client.HttpClientOption{
		tls_client.WithTimeoutSeconds(30),
		tls_client.WithClientProfile(profiles.CloudflareCustom),
		tls_client.WithNotFollowRedirects(),
		tls_client.WithCookieJar(jar), // create cookieJar instance and pass it as argument
	}

	client, err := tls_client.NewHttpClient(tls_client.NewNoopLogger(), options...)
	if err != nil {
		panic(err)
	}
	return client
}
