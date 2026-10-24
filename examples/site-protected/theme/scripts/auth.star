def admin(input):
    """Signed out goes to /login/; signed in but not admin is refused."""
    user = input["ctx"].get("user")
    if not user:
        brick.redirect("/login/")
    if user != "admin":
        brick.error(403, "Admins only.")
    return {"name": user}
