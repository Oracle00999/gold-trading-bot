package main

const goldSymbol = "XAUUSDm"
const euroSymbol = "EURUSDm"
const yenSymbol = "USDJPYm"
const bitcoinSymbol = "BTCUSDm"

func supportedSymbol(symbol string) bool {
	return symbol == goldSymbol || symbol == euroSymbol || symbol == yenSymbol || symbol == bitcoinSymbol
}
func selectedSymbol(symbols []string) string {
	if len(symbols) > 0 {
		return symbols[0]
	}
	return goldSymbol
}
func targetDistance(symbol string) float64 {
	if symbol == euroSymbol {
		return .0005
	}
	if symbol == yenSymbol {
		return .05
	}
	if symbol == bitcoinSymbol {
		return 250
	}
	return 5
}
