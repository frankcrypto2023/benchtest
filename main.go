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
	"sync"
	"sync/atomic"
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
var allCount int64
var failCount int64
var succCount int64
var pageSize int

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

func SendTx(index, i int, client *ethclient.Client, from string, toAddress common.Address, value *big.Int) {
	atomic.AddInt64(&allCount, 1)
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
		// failCount++
		atomic.AddInt64(&failCount, 1)
		fmt.Printf("\nminer:%d, index:%d, tx sent error: %s %v\n", index, i, signedTx.Hash().Hex(), err)
		return
	}
	atomic.AddInt64(&succCount, 1)
	// fmt.Printf("\n miner:%d, index:%d, tx sent: %s \n", index, i, signedTx.Hash().Hex())
}

var rpcClients = []*ethclient.Client{}

func InitRPC() {
	for i := 0; i < 4; i++ {
		if i == 0 {
			c, _ := ethclient.Dial(os.Getenv("RPC"))
			rpcClients = append(rpcClients, c)
			continue
		}
		if os.Getenv(fmt.Sprintf("RPC%d", i)) != "" {
			c, _ := ethclient.Dial(os.Getenv(fmt.Sprintf("RPC%d", i)))
			rpcClients = append(rpcClients, c)
		}
	}
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()
	err := godotenv.Load(".env")
	if err != nil {
		log.Fatalf("Some error occured. Err: %s", err)
	}
	InitRPC()
	pageSize, _ = strconv.Atoi(os.Getenv("pageSize"))
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
	fmt.Printf("\nSend %d txs every miner / s, miner count:%d\n", pageSize, len(rpcClients))

	if os.Getenv("init") == "1" {
		for i, v := range keys {
			ba, _ := client.BalanceAt(context.Background(), common.HexToAddress(v.A), nil)
			if ba.Cmp(big.NewInt(1e15)) <= 0 {
				fmt.Println("--------------------------------------------------------------------------init account index", i)
				SendTx(0, i, client, os.Getenv("PRIVATE_KEY"), common.HexToAddress(v.A), big.NewInt(2e18))
				continue
			}
		}
	}

	ctx := context.Background()
	wg := sync.WaitGroup{}
	for i := 0; i < len(rpcClients); i++ {
		index := i % len(rpcClients)
		wg.Add(1)
		go AccountSend(&wg, ctx, index)
	}
	go SendStats(&wg, ctx)
	wg.Wait()
}

// AccountSend(ctx context.Context, index int)
func AccountSend(wg *sync.WaitGroup, ctx context.Context, index int) {
	defer wg.Done()
	fmt.Printf("\n-----------------------------------------------------------------miner:%d\n", index)
	rpcClient := rpcClients[index]
	var to common.Address
	if index == len(keys)-1 {
		to = common.HexToAddress(keys[0].A)
	} else {
		to = common.HexToAddress(keys[index+1].A)
	}
	offset := pageSize * index
	for {
		for i := offset; i < pageSize*(index+1); i++ {
			select {
			case <-ctx.Done():
				return
			default:
				v := keys[i]
				// fmt.Println(v.A, to)
				go SendTx(index, i, rpcClient, v.K, to, big.NewInt(1e1))
				time.Sleep(time.Second / time.Duration(pageSize))
			}
		}
		log.Println("----------------------------------------------------------------------miner", index, "send txCount", pageSize)
	}
	//
}
func SendStats(wg *sync.WaitGroup, ctx context.Context) {
	defer wg.Done()
	t := time.NewTicker(time.Second * 10)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			log.Println("----------------------------------------------------------------------allCount", allCount, "successCount", succCount, "failCount", failCount)
		}
	}
	//
}
