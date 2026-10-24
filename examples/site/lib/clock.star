# now() is a capability the host granted in Go (Options.Globals); scripts
# have no clock of their own.
def tick(input):
    return {"time": now()}
