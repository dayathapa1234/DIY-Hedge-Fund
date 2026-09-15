#!/usr/bin/env python3
import datetime
import json
import sys

import yfinance as yf


def main():
    if len(sys.argv) < 2 or not sys.argv[1].strip():
        print("symbol is required", file=sys.stderr)
        return 2

    symbol = sys.argv[1].strip().upper()
    start = sys.argv[2].strip() if len(sys.argv) > 2 else ""
    end = sys.argv[3].strip() if len(sys.argv) > 3 else ""
    interval = sys.argv[4].strip() if len(sys.argv) > 4 and sys.argv[4].strip() else "1d"
    ticker = yf.Ticker(symbol)
    options = {"interval": interval, "auto_adjust": False}
    if start:
        options["start"] = start
    if end:
        end_date = datetime.date.fromisoformat(end) + datetime.timedelta(days=1)
        options["end"] = end_date.isoformat()
    if not start and not end:
        options["period"] = "max"

    history = ticker.history(**options)
    if history.empty:
        print("no data", file=sys.stderr)
        return 1

    prices = []
    for index, row in history.iterrows():
        prices.append(
            {
                "date": index.date().isoformat() + "T00:00:00Z",
                "open": float(row.get("Open", 0) or 0),
                "high": float(row.get("High", 0) or 0),
                "low": float(row.get("Low", 0) or 0),
                "close": float(row.get("Close", 0) or 0),
                "adjClose": float(row.get("Adj Close", row.get("Close", 0)) or 0),
                "volume": int(row.get("Volume", 0) or 0),
            }
        )

    print(json.dumps({"symbol": symbol, "provider": "yfinance", "interval": interval, "prices": prices}))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
