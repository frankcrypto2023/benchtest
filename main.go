package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/joho/godotenv"
)

type PV struct {
	K string `json:"k"`
	A string `json:"a"`
}

var keys []PV
var keyFile = "keys.json"

func load() {
	b, err := os.ReadFile(keyFile)
	if err != nil {
		log.Fatalln(keyFile, " read file error", err)
		return
	}
	_ = json.Unmarshal(b, &keys)
}

func createPrivkeys() {
	for i := 0; i < 3000; i++ {
		// 生成随机的私钥
		privateKey, err := crypto.GenerateKey()
		if err != nil {
			panic(err)
		}

		// 使用私钥生成地址
		address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()

		// 将私钥转换为hex编码的字符串
		privateKeyStr := hexutil.Encode(crypto.FromECDSA(privateKey))

		// 打印生成的私钥、地址和助记词
		fmt.Printf("Private Key: %s\n", privateKeyStr)
		fmt.Printf("Address: %s\n", address)
		keys = append(keys, PV{
			K: privateKeyStr[2:],
			A: address,
		})
		b, _ := json.Marshal(keys)
		os.WriteFile(keyFile, b, 0755)
	}
}

func SendTx(client *ethclient.Client, from string, toAddress common.Address, value *big.Int) {
	privateKey, err := crypto.HexToECDSA(from)
	if err != nil {
		log.Fatal(err)
	}
	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		log.Fatal("cannot assert type: publicKey is not of type *ecdsa.PublicKey")
	}

	fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)
	nonce, err := client.PendingNonceAt(context.Background(), fromAddress)
	if err != nil {
		log.Fatal(err)
	}

	gasLimit := uint64(21000) // in units
	gasPrice, err := client.SuggestGasPrice(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	var data []byte
	tx := types.NewTransaction(nonce, toAddress, value, gasLimit, gasPrice, data)

	chainID, err := client.ChainID(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), privateKey)
	if err != nil {
		log.Fatal(err)
	}

	err = client.SendTransaction(context.Background(), signedTx)
	if err != nil {
		fmt.Printf("tx sent error: %s %v", signedTx.Hash().Hex(), err)
	}

	fmt.Printf("\ntx sent: %s\n", signedTx.Hash().Hex())
}

func main() {
	err := godotenv.Load(".env")
	if err != nil {
		log.Fatalf("Some error occured. Err: %s", err)
	}
	client, err := ethclient.Dial(os.Getenv("RPC"))
	if err != nil {
		log.Fatal(err)
	}
	if len(os.Args) > 1 {
		action := os.Args[1]
		switch action {
		case "init":
			createPrivkeys()
			return
		}
	}
	load()
	maxStr := os.Getenv("MAX")
	max, _ := strconv.ParseInt(maxStr, 10, 64)
	for {
		count := 0
		for i, v := range keys {
			ba, _ := client.BalanceAt(context.Background(), common.HexToAddress(v.A), nil)
			if ba.Cmp(big.NewInt(1e7)) <= 0 {
				SendTx(client, os.Getenv("PRIVATE_KEY"), common.HexToAddress(v.A), big.NewInt(2e18))
				count++
				continue
			}
			var to common.Address
			if i == len(keys)-1 {
				to = common.HexToAddress(keys[0].A)
			} else {
				to = common.HexToAddress(keys[i+1].A)
			}
			go SendTx(client, v.K, to, big.NewInt(1e1))
			count++
			if count >= int(max) {
				time.Sleep(10 * time.Second)
			}
		}
	}
}
