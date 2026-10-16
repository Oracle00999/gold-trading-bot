package main

const goldSymbol = "XAUUSDm"
const euroSymbol = "EURUSDm"
const yenSymbol = "USDJPYm"

func supportedSymbol(symbol string) bool {
	return symbol == goldSymbol || symbol == euroSymbol || symbol == yenSymbol
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
	return 5
}
