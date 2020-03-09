package internal

const (
	// CreateMatchesTable creates the matches table for storing data related to
	// a match and the related swap. This only includes trade matches, not
	// cancel order matches that just remove one order from the book (and change
	// the target order status in the orders table).
	//
	// The takerSell column indicates the asset of the address and coinID
	// columns for both maker and taker. Sell refers to sell of the base asset,
	// and the opposite is implied for the counterparty (makerSell = !takerSell)
	//
	//   takerSell   | takerAddress | aContractCoinID | aRedeemCoinID ||  (makerSell)  | makerAddress | bContractCoinID | bRedeemCoinID
	// ---------------------------------------------------------------------------------------------------------------------------------
	//  true (B->Q)  |    quote     |      base       |     quote     ||  false (Q->B) |     base     |      quote      |     base
	//  false (Q->B) |    base      |      quote      |     base      ||  true (B->Q)  |     quote    |      base       |     quote
	CreateMatchesTable = `CREATE TABLE IF NOT EXISTS %s (
		matchid BYTEA PRIMARY KEY,
		takerSell BOOL,        -- to identify asset of address and coinIDs
		takerOrder BYTEA,      -- INDEX this
		takerAccount BYTEA,    -- INDEX this
		takerAddress TEXT,
		makerOrder BYTEA,      -- INDEX this
		makerAccount BYTEA,    -- INDEX this
		makerAddress TEXT,
		epochIdx INT8,
		epochDur INT8,
		quantity INT8,
		rate INT8,
		status INT2,           -- also updated during swap negotiation

		-- The remaining columns are only set during swap negotiation.
		sigMatchAckMaker BYTEA,   -- maker's ack of the match
		sigMatchAckTaker BYTEA,   -- taker's ack of the match

		-- initiator/A (maker) CONTRACT data
		aContractCoinID BYTEA,    -- coinID (e.g. tx:vout) with the contract
		aContract BYTEA,          -- includes secret hash, get with ExtractSwapDetails for DCR
		aContractTime INT8,       -- server time stamp
		bSigAckOfAContract BYTEA, -- counterparty's (participant) sig with ack of initiator CONTRACT data

		-- participant/B (taker) CONTRACT data
		bContractCoinID BYTEA,
		bContract BYTEA,
		bContractTime INT8,       -- server time stamp
		aSigAckOfBContract BYTEA, -- counterparty's (initiator) sig with ack of participant CONTRACT data

		-- initiator/A (maker) REDEEM data
		aRedeemCoinID BYTEA,      -- the input spending the taker's contract output includes the secret
		aRedeemTime INT8,         -- server time stamp
		bSigAckOfARedeem BYTEA,   -- counterparty's (participant) sig with ack of initiator REDEEM data

		-- participant/B (taker) REDEEM data
		bRedeemCoinID BYTEA,
		bRedeemTime INT8,         -- server time stamp
		aSigAckOfBRedeem BYTEA    -- counterparty's (initiator) sig with ack of participant REDEEM data
	)`

	RetrieveSwapData = `SELECT status, sigMatchAckMaker, sigMatchAckTaker,
		aContractCoinID, aContract, aContractTime, bSigAckOfAContract,
		bContractCoinID, bContract, bContractTime, aSigAckOfBContract,
		aRedeemCoinID, aRedeemTime, bSigAckOfARedeem,
		bRedeemCoinID, bRedeemTime, aSigAckOfBRedeem
	FROM %s WHERE matchid = $1;`

	InsertMatch = `INSERT INTO %s (matchid,
		takerOrder, takerAccount, takerAddress,
		makerOrder, makerAccount, makerAddress,
		epochIdx, epochDur,
		quantity, rate, status)
	VALUES ($1,
		$2, $3, $4,
		$5, $6, $7,
		$8, $9,
		$10, $11, $12) `  // do not terminate with ;

	UpsertMatch = InsertMatch + ` ON CONFLICT (matchid) DO
	UPDATE SET quantity = $10, status = $12;`

	SelectActiveMatches = `SELECT makerOrder, takerOrder
		FROM %s WHERE status = %d;`

	RetrieveMatchByID = `SELECT matchid, takerOrder, takerAccount, takerAddress,
		makerOrder, makerAccount, makerAddress, epochIdx, epochDur, quantity, rate, status
	FROM %s WHERE matchid = $1;`

	RetrieveUserMatches = `SELECT matchid, takerOrder, takerAccount, takerAddress,
		makerOrder, makerAccount, makerAddress, epochIdx, epochDur, quantity, rate, status
	FROM %s WHERE takerAccount = $1 OR makerAccount = $1;`

	RetrieveActiveUserMatches = `SELECT matchid, takerOrder, takerAccount, takerAddress,
		makerOrder, makerAccount, makerAddress, epochIdx, epochDur, quantity, rate, status
	FROM %s
	WHERE (takerAccount = $1 OR makerAccount = $1)
		AND status <> $2;`  // $2 should the code for MatchComplete

	SetMakerMatchAckSig = `UPDATE %s SET sigMatchAckMaker = $2 WHERE matchid = $1;`
	SetTakerMatchAckSig = `UPDATE %s SET sigMatchAckTaker = $2 WHERE matchid = $1;`

	SetInitiatorSwapData = `UPDATE %s SET status = $2,
		aContractCoinID = $3, aContract = $4, aContractTime = $5
	WHERE matchid = $1;`
	SetParticipantSwapData = `UPDATE %s SET status = $2,
		bContractCoinID = $3, bContract = $4, bContractTime = $5
	WHERE matchid = $1;`

	SetParticipantContractAuditSig = `UPDATE %s SET bSigAckOfAContract = $2 WHERE matchid = $1;`
	SetInitiatorContractAuditSig   = `UPDATE %s SET aSigAckOfBContract = $2 WHERE matchid = $1;`

	SetInitiatorRedeemData = `UPDATE %s SET status = $2,
		aRedeemCoinID = $3, aRedeemTime = $4
	WHERE matchid = $1;`
	SetParticipantRedeemData = `UPDATE %s SET status = $2,
		bRedeemCoinID = $3, bRedeemTime = $4
	WHERE matchid = $1;`

	SetParticipantRedeemAckSig = `UPDATE %s SET bSigAckOfARedeem = $2 WHERE matchid = $1;`
	SetInitiatorRedeemAckSig   = `UPDATE %s SET aSigAckOfBRedeem = $2 WHERE matchid = $1;`
)
