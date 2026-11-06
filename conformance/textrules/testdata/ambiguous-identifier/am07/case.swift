struct ClickEventProbe {
    let target: String
}

func onClickProbe(_ e: ClickEventProbe) -> String {
    e.target
}
