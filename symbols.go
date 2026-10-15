package main

const goldSymbol = "XAUUSDm"
const euroSymbol = "EURUSDm"

func supportedSymbol(symbol string) bool { return symbol == goldSymbol || symbol == euroSymbol }
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
	return 5
}
