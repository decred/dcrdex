// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package market

// The Market[Manager] will:
// - Cycle the epochs. Epoch timing, the current epoch index, etc.
// - Manage multiple epochs (one active and others in various states of
//   matching, swapping, or archival).
// - Receiving and validating new order data (amounts vs. lot size, check fees,
//   utxos, sufficient market buy buffer, etc. with help from asset backends).
// - Putting incoming orders into the current epoch queue, which must implement
//   matcher.Booker so that the order matching engine can work with it.
// - Possess an order book manager, which must also implement matcher.Booker.
// - Initiate order matching via matcher.Match(book, currentQueue)
// - During and/or after matching:
//     * update the book (remove orders, add new standing orders, etc.)
//     * retire/archive the epoch queue
//     * publish the matches (and order book changes?)
//     * initiate swaps for each match (possibly groups of related matches)
// - Continually update the order book based on data from the swap executors
//   (e.g. failed swaps, partial fills?, etc.), communications hub (i.e. dropped
//   clients), and other sources.
// - Recording all events with the archivist

// The Market manager should not be overly involved with details of accounts and
// authentication. Via the account package it should request account status with
// new orders, verification of order signatures. The Market should also perform
// various account package callbacks such as order status updates so that the
// account package code can keep various data up-to-date, including order
// status, history, cancellation statistics, etc.
type Market struct{} // TODO
