package api

// Money is an amount in hundredths of a cent: a bog-hour at cost is a
// fraction of a cent, and integers keep sums exact.
type Money int64

// MoneyPerDollar is how many Money make a US dollar.
const MoneyPerDollar Money = 10000
