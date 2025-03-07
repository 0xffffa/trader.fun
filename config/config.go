package config

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

type Config struct {
	TraderPVTK    string  `json:"traderPvtK"`
	TraderPUBK    string  `json:"traderPubK"`
	RPCEndpoint   string  `json:"rpcEndpoint"`
	TotalStopLoss float64 `json:"totalStopLoss"`
	TradeStopLoss float64 `json:"tradeStopLoss"`
	BalanceRisk   float64 `json:"balanceRisk"`
	Traders       int     `json:"traders"`
	Slippage      float64 `json:"slippage"`
}

var (
	defaultWallet  = solana.NewWallet()
	configFileName = "config.json"
	defaultConfig  = Config{
		TraderPVTK:    defaultWallet.PrivateKey.String(),
		TraderPUBK:    defaultWallet.PublicKey().String(),
		RPCEndpoint:   rpc.MainNetBeta_RPC,
		BalanceRisk:   4.0,  // 4%
		TradeStopLoss: 20.0, // 20%
		TotalStopLoss: 10.0, // 10%
		Traders:       1,
		Slippage:      0.04, // 4%
	}
)

func LoadConfig() *Config {
	if _, err := os.Stat(configFileName); os.IsNotExist(err) {
		file, err := os.Create(configFileName)
		if err != nil {
			fmt.Println("Error creating config file:", err)
			os.Exit(1)
		}
		defer file.Close()

		configData, err := json.MarshalIndent(defaultConfig, "", "  ")
		if err != nil {
			fmt.Println("Error marshalling default config:", err)
			os.Exit(1)
		}
		file.Write(configData)

		return &defaultConfig
	}

	return loadConfigFromFile()
}

func loadConfigFromFile() *Config {
	data, err := ioutil.ReadFile(configFileName)
	if err != nil {
		fmt.Println("Error reading config file:", err)
		os.Exit(1)
	}

	var config Config
	err = json.Unmarshal(data, &config)
	if err != nil {
		fmt.Println("Error unmarshalling config:", err)
		os.Exit(1)
	}

	return &config
}
