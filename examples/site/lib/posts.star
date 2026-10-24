load("~/data/posts.yaml", all = "data")

def recent(input):
    return {"items": all[:input["props"]["limit"]]}
