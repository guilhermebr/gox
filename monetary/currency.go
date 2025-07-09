package monetary

var (
	BRL = NewAsset("BRL", 2, "R$", "currency")
	USD = NewAsset("USD", 2, "$", "currency")
	GBP = NewAsset("GBP", 2, "£", "currency")
	CHF = NewAsset("CHF", 2, "CHF", "currency")
	JPY = NewAsset("JPY", 0, "¥", "currency")
	ARS = NewAsset("ARS", 2, "$", "currency")
	CLP = NewAsset("CLP", 0, "$", "currency")
	CAD = NewAsset("CAD", 2, "$", "currency")
	MXN = NewAsset("MXN", 2, "$", "currency")
	COP = NewAsset("COP", 2, "$", "currency")
)
