export class Base {
  do_work(): void {}
}

export class Child extends Base {
  override do_work(): void {}
}
