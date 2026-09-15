package cache

import (
	"fmt"
	"market-data-service/internal/marketdata"
)

func Key(query marketdata.HistoricalQuery) string {
	return fmt.Sprintf("market-data:%s|%s|%s|%s|%s|%v", query.Symbol, query.Interval, query.Provider, query.From.Format("2006-01-02"), query.To.Format("2006-01-02"), query.Priority)
}
