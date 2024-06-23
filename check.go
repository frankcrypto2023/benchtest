package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/sirupsen/logrus"
)

type Mempool struct {
	Jsonrpc string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  []string    `json:"result"`
}

type GBTResponse struct {
	Jsonrpc string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  struct {
		Stateroot         string `json:"stateroot"`
		Curtime           int    `json:"curtime"`
		Height            int    `json:"height"`
		Blues             int    `json:"blues"`
		Previousblockhash string `json:"previousblockhash"`
		Sigoplimit        int    `json:"sigoplimit"`
		Sizelimit         int    `json:"sizelimit"`
		Weightlimit       int    `json:"weightlimit"`
		Parents           []struct {
			Data string `json:"data"`
			Hash string `json:"hash"`
		} `json:"parents"`
		Transactions []struct {
			Data    string        `json:"data"`
			Hash    string        `json:"hash"`
			Depends []interface{} `json:"depends"`
			Fee     int           `json:"fee"`
			Sigops  int           `json:"sigops"`
			Weight  int           `json:"weight"`
		} `json:"transactions"`
		Version     int `json:"version"`
		Coinbaseaux struct {
			Flags string `json:"flags"`
		} `json:"coinbaseaux"`
		Coinbasevalue    int    `json:"coinbasevalue"`
		Nodeinfo         string `json:"nodeinfo"`
		Longpollid       string `json:"longpollid"`
		PowDiffReference struct {
			Nbits  string `json:"nbits"`
			Target string `json:"target"`
		} `json:"pow_diff_reference"`
		Maxtime      int      `json:"maxtime"`
		Mintime      int      `json:"mintime"`
		Mutable      []string `json:"mutable"`
		Noncerange   string   `json:"noncerange"`
		Workdata     string   `json:"workdata"`
		Capabilities []string `json:"capabilities"`
		BlockFeesMap struct {
		} `json:"block_fees_map"`
		CoinbaseVersion string `json:"coinbase_version"`
	} `json:"result"`
}

func (g *GBTResponse) HasTx(txid string) bool {
	return len(g.Result.Transactions) > 0
}

func CheckTxTime(client *ethclient.Client, ctx context.Context, adminPV *PV) {
	go CheckTime(ctx)
	for i, v := range keys {
		adminPV.SendTx(ctx, 0, i, client, common.HexToAddress(v.A), big.NewInt(1e5), 0, nil, nil)
	}
}

func CheckGBTEmptyDuration(ctx context.Context, rpc, auth string, client *ethclient.Client) {
	t := time.NewTicker(time.Second / 10)
	defer t.Stop()
	lastEmptyTime := int64(0)
	emptyDurations := make([]int64, 0)
	lastDisplay := time.Now()
	allEmptyDurations := int64(0)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b := RpcResult(rpc, auth, "getBlockTemplate", []interface{}{[]interface{}{}, 8}, "1")
			if b != nil {
				var bc GBTResponse
				err := json.Unmarshal(b, &bc)
				if err != nil {
					logrus.Error("gbt error", err)
					continue
				}
				if len(bc.Result.Transactions) < 1 && lastEmptyTime == 0 {
					lastEmptyTime = time.Now().UnixMilli()
				}
				if len(bc.Result.Transactions) > 0 && lastEmptyTime > 0 {
					allEmptyDurations += time.Now().UnixMilli() - lastEmptyTime
					emptyDurations = append(emptyDurations, time.Now().UnixMilli()-lastEmptyTime)
					lastEmptyTime = 0
				}
				if time.Since(lastDisplay).Seconds() >= 60 && len(emptyDurations) > 0 {
					fmt.Println(fmt.Sprintf("------------------------%s 当前gbt为空时间隔:%d ms ,空次数:%d", rpc, allEmptyDurations/int64(len(emptyDurations)), len(emptyDurations)))
					lastDisplay = time.Now()
				}
			}
		}
	}
}

func CheckMemEmptyDuration(ctx context.Context, rpc, auth string, client *ethclient.Client) {
	t := time.NewTicker(time.Second / 10)
	defer t.Stop()
	lastEmptyTime := int64(0)
	emptyDurations := make([]int64, 0)
	lastDisplay := time.Now()
	allEmptyDurations := int64(0)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b := RpcResult(rpc, auth, "getMempool", []interface{}{"", true}, "1")
			if b != nil {
				var bc Mempool
				err := json.Unmarshal(b, &bc)
				if err != nil {
					logrus.Error("gbt error", err)
					continue
				}
				if len(bc.Result) < 1 && lastEmptyTime == 0 {
					lastEmptyTime = time.Now().UnixMilli()
				}
				if len(bc.Result) > 0 && lastEmptyTime > 0 {
					allEmptyDurations += time.Now().UnixMilli() - lastEmptyTime
					emptyDurations = append(emptyDurations, time.Now().UnixMilli()-lastEmptyTime)
					lastEmptyTime = 0
				}
				if time.Since(lastDisplay).Seconds() >= 60 && len(emptyDurations) > 0 {
					fmt.Println(fmt.Sprintf("------------------------%s 当前mempool为空时间隔:%d ms ,为空次数:%d", rpc, allEmptyDurations/int64(len(emptyDurations)), len(emptyDurations)))
					lastDisplay = time.Now()
				}
			}
		}
	}
}

func CheckTime(ctx context.Context) {
	if len(rpcClients) < 3 || len(RPCS) < 3 {
		logrus.Error("miner Not Enough")
		return
	}
	<-time.After(time.Second * 3)
	go func() {
		CheckGBTEmptyDuration(ctx, JRPCS[0], JAUTH[0], rpcClients[0])
	}()
	go func() {
		CheckGBTEmptyDuration(ctx, JRPCS[1], JAUTH[1], rpcClients[1])
	}()
	go func() {
		CheckGBTEmptyDuration(ctx, JRPCS[2], JAUTH[2], rpcClients[2])
	}()
	go func() {
		CheckMemEmptyDuration(ctx, JRPCS[0], JAUTH[0], rpcClients[0])
	}()
	go func() {
		CheckMemEmptyDuration(ctx, JRPCS[1], JAUTH[1], rpcClients[1])
	}()
	go func() {
		CheckMemEmptyDuration(ctx, JRPCS[2], JAUTH[2], rpcClients[2])
	}()
}
