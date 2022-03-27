// Copyright 2019 cruzbit developers
// Use of this source code is governed by a MIT-style license that can be found in the LICENSE file.

package cruzbit

// the below values affect ledger consensus and come directly from bitcoin.
// we could have played with these but we're introducing significant enough changes
// already IMO, so let's keep the scope of this experiment as small as we can

const CruzbitsPerCruz = 100000000

const InitialCoinbaseReward = 50 * CruzbitsPerCruz

const CoinbaseMaturity = 100 // blocks

const InitialTarget = "00000000ffff0000000000000000000000000000000000000000000000000000"

const MaxFutureSeconds = 2 * 60 * 60 // 2 hours

const MaxMoney = 21000000 * CruzbitsPerCruz

const RetargetInterval = 2016 // 2 weeks in blocks

const RetargetTime = 1209600 // 2 weeks in seconds

const TargetSpacing = 600 // every 10 minutes

const NumBlocksForMedianTmestamp = 11

const BlocksUntilRewardHalving = 210000 // 4 years in blocks

// the below value affects ledger consensus and comes from bitcoin cash

const RetargetSmaWindow = 144 // 1 day in blocks

// the below values affect ledger consensus and are new as of our ledger

const InitialMaxTransactionsPerBlock = 10000 // 16.666... tx/sec, ~4 MBish in JSON

const BlocksUntilTransactionsPerBlockDoubling = 105000 // 2 years in blocks

const MaxTransactionsPerBlock = 1<<31 - 1

const MaxTransactionsPerBlockExceededAtHeight = 1852032 // pre-calculated

const BlocksUntilNewSeries = 1008 // 1 week in blocks

const MaxMemoLength = 100 // bytes (ascii/utf8 only)

// given our JSON protocol we should respect Javascript's Number.MAX_SAFE_INTEGER value
const MaxNumber int64 = 1<<53 - 1

// height at which we switch from bitcoin's difficulty adjustment algorithm to bitcoin cash's algorithm
const BitcoinCashRetargetAlgorithmHeight = 28861

// the below values only affect peering behavior and do not affect ledger consensus

const DefaultCruzbitPort = 8831

const MaxOutboundPeerConnections = 8

const MaxInboundPeerConnections = 128

const MaxInboundPeerConnectionsFromSameHost = 4

// MaxTipAge is originally 24 hours, but has been increased to 30 days to prevent deadlock caused by low mining volume
const MaxTipAge = 24 * 60 * 60 * 30

const MaxProtocolMessageLength = 2 * 1024 * 1024 // doesn't apply to blocks

// the below values are mining policy and also do not affect ledger consensus

// if you change this it needs to be less than the maximum at the current height
const MaxTransactionsToIncludePerBlock = InitialMaxTransactionsPerBlock

const MaxTransactionQueueLength = MaxTransactionsToIncludePerBlock * 10

const MinFeeCruzbits = 1000000 // 0.01 cruz

const MinAmountCruzbits = 1000000 // 0.01 cruz
