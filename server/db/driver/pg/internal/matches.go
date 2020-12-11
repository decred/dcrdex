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
		active BOOL DEFAULT TRUE,    -- negotiation active, where FALSE includes failure, successful completion, or a taker cancel order
		takerSell BOOL,        -- to identify asset of address and coinIDs, NULL for cancel orders
		takerOrder BYTEA,      -- INDEX this
		takerAccount BYTEA,    -- INDEX this
		takerAddress TEXT,     -- NULL for cancel orders
		makerOrder BYTEA,      -- INDEX this
		makerAccount BYTEA,    -- INDEX this
		makerAddress TEXT,     -- NULL for cancel orders
		epochIdx INT8,
		epochDur INT8,
		quantity INT8,
		rate INT8,
		baseRate INT8, quoteRate INT8, -- contract tx fee rates, NULL for cancel orders
		status INT2,           -- also updated during swap negotiation, independent from active for failed swaps
		forgiven BOOL,

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
		aRedeemSecret BYTEA,
		aRedeemTime INT8,         -- server time stamp
		bSigAckOfARedeem BYTEA,   -- counterparty's (participant) sig with ack of initiator REDEEM data

		-- participant/B (taker) REDEEM data
		bRedeemCoinID BYTEA,
		bRedeemTime INT8          -- server time stamp
	)`

	AddMatchesForgivenColumn = `ALTER TABLE %s
		ADD COLUMN IF NOT EXISTS forgiven BOOL;`

	RetrieveSwapData = `SELECT status, sigMatchAckMaker, sigMatchAckTaker,
		aContractCoinID, aContract, aContractTime, bSigAckOfAContract,
		bContractCoinID, bContract, bContractTime, aSigAckOfBContract,
		aRedeemCoinID, aRedeemSecret, aRedeemTime, bSigAckOfARedeem,
		bRedeemCoinID, bRedeemTime
	FROM %s WHERE matchid = $1;`

	InsertMatch = `INSERT INTO %s (matchid, takerSell,
		takerOrder, takerAccount, takerAddress,
		makerOrder, makerAccount, makerAddress,
		epochIdx, epochDur,
		quantity, rate, baseRate, quoteRate, status)
	VALUES ($1, $2,
		$3, $4, $5,
		$6, $7, $8,
		$9, $10,
		$11, $12, $13, $14, $15) `  // do not terminate with ;

	UpsertMatch = InsertMatch + ` ON CONFLICT (matchid) DO
	UPDATE SET quantity = $11, status = $15;`

	InsertCancelMatch = `INSERT INTO %s (matchid, active, -- omit takerSell
			takerOrder, takerAccount, -- no taker address for a cancel order
			makerOrder, makerAccount, -- omit maker's swap address too
			epochIdx, epochDur,
			quantity, rate, status) -- omit base and quote fee rates
		VALUES ($1, FALSE, -- no active swap for a cancel
			$2, $3,
			$4, $5,
			$6, $7,
			$8, $9, $10) `  // status should be MatchComplete although there is no swap

	UpsertCancelMatch = InsertCancelMatch + ` ON CONFLICT (matchid) DO NOTHING;`

	RetrieveMatchByID = `SELECT matchid, active, takerSell,
		takerOrder, takerAccount, takerAddress,
		makerOrder, makerAccount, makerAddress,
		epochIdx, epochDur, quantity, rate, baseRate, quoteRate, status
	FROM %s WHERE matchid = $1;`

	RetrieveUserMatches = `SELECT matchid, active, takerSell,
		takerOrder, takerAccount, takerAddress,
		makerOrder, makerAccount, makerAddress,
		epochIdx, epochDur, quantity, rate, baseRate, quoteRate, status
	FROM %s
	WHERE takerAccount = $1 OR makerAccount = $1;`

	RetrieveActiveUserMatches = `SELECT matchid, takerSell,
		takerOrder, takerAccount, takerAddress,
		makerOrder, makerAccount, makerAddress,
		epochIdx, epochDur, quantity, rate, baseRate, quoteRate, status
	FROM %s
	WHERE (takerAccount = $1 OR makerAccount = $1)
		AND active;`

	RetrieveMarketMatches = `SELECT matchid, active, takerSell,
		takerOrder, takerAccount, takerAddress,
		makerOrder, makerAccount, makerAddress,
		epochIdx, epochDur, quantity, rate, baseRate, quoteRate, status
	FROM %s
	WHERE takerSell IS NOT NULL -- not a cancel order
	ORDER BY epochIdx * epochDur DESC;`

	RetrieveActiveMarketMatches = `SELECT matchid, takerSell,
		takerOrder, takerAccount, takerAddress,
		makerOrder, makerAccount, makerAddress,
		epochIdx, epochDur, quantity, rate, baseRate, quoteRate, status
	FROM %s
	WHERE takerSell IS NOT NULL -- not a cancel order
		AND active
	ORDER BY epochIdx * epochDur DESC;`

	// RetrieveActiveMarketMatchesExtended combines RetrieveSwapData with
	// RetrieveActiveMarketMatches.
	RetrieveActiveMarketMatchesExtended = `SELECT matchid, takerSell,
		takerOrder, takerAccount, takerAddress,
		makerOrder, makerAccount, makerAddress,
		epochIdx, epochDur, quantity, rate, baseRate, quoteRate, status,
		sigMatchAckMaker, sigMatchAckTaker,
		aContractCoinID, aContract, aContractTime, bSigAckOfAContract,
		bContractCoinID, bContract, bContractTime, aSigAckOfBContract,
		aRedeemCoinID, aRedeemSecret, aRedeemTime, bSigAckOfARedeem,
		bRedeemCoinID, bRedeemTime
	FROM %s
	WHERE takerSell IS NOT NULL -- not a cancel order
		AND active
	ORDER BY epochIdx * epochDur DESC;`

	// CompletedOrAtFaultMatchesLastN retrieves inactive matches for a user that
	// are either successfully completed by the user (MatchComplete or
	// MakerRedeemed with user as maker), or failed because of this user's
	// inaction. Note that the literal status values used in this query MUST BE
	// UPDATED if the order.OrderStatus enum is changed.
	CompletedOrAtFaultMatchesLastN = `
		WITH acct (aid) AS ( VALUES($1::BYTEA) )

		SELECT matchid, status, quantity, (status=4 OR (status=3 AND makerAccount = aid AND takerAccount != aid)) AS success,
			GREATEST((epochIdx+1)*epochDur, aContractTime, bContractTime, aRedeemTime, bRedeemTime) AS lastTime
		FROM %s, acct
		WHERE takerSell IS NOT NULL      -- exclude cancel order matches
			AND (makerAccount = aid OR takerAccount = aid)
			AND (
				-- swap success for both
				status=4                                       -- success for both
				OR
				-- swap success for maker unless maker==taker
				(status=3 AND makerAccount = aid AND takerAccount != aid)
				OR
				( -- at-fault swap failures
					NOT active -- failure means inactive/revoked
					AND (forgiven IS NULL OR NOT forgiven)
					AND (
						(status=0 AND makerAccount = aid) OR   -- fault for maker
						(status=1 AND takerAccount = aid) OR   -- fault for taker
						(status=2 AND makerAccount = aid) OR   -- fault for maker
						(status=3 AND takerAccount = aid)      -- fault for taker
					)
				)
			)
		ORDER BY lastTime DESC   -- last action time i.e. success or approx. when could have acted
		LIMIT $2;`

	ForgiveMatchFail = `UPDATE %s SET forgiven = TRUE
		WHERE matchid = $1 AND NOT active;`

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
		aRedeemCoinID = $3, aRedeemSecret = $4, aRedeemTime = $5
	WHERE matchid = $1;`
	SetParticipantRedeemData = `UPDATE %s SET status = $2,
		bRedeemCoinID = $3, bRedeemTime = $4, active = FALSE
	WHERE matchid = $1;`

	SetParticipantRedeemAckSig = `UPDATE %s
		SET bSigAckOfARedeem = $2
		WHERE matchid = $1;`

	SetSwapDone = `UPDATE %s SET active = FALSE
		WHERE matchid = $1;`

	SelectMatchStatuses = `SELECT matchid, status, aContract, bContract, aContractCoinID,
		bContractCoinID, aRedeemCoinID, bRedeemCoinID, aRedeemSecret, active
		FROM %s
		WHERE matchid = ANY($2)
		AND (takerAccount = $1 OR makerAccount = $1);`
)
