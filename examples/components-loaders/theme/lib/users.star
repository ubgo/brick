USERS = {"1": "Ada", "2": "Grace", "3": "Linus"}

def cards(inputs):
    return [{"name": USERS[i["props"]["id"]]} for i in inputs]
