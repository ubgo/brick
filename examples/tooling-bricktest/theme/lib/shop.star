# price is Price's loader: it returns the props the template sees beside
# the ones the call site passed. The currency is never printed, so only
# the view log can show that it reached the template.
def price(input):
    return {"amount": "12.00", "currency": "EUR"}
