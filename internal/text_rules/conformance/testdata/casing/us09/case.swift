class BaseProbe {
    func do_work() {}
}

final class ChildProbe: BaseProbe {
    override func do_work() {}
}
