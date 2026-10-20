declare const translationSlotsBrand: unique symbol;
type Brand = typeof translationSlotsBrand;
interface Template { readonly [translationSlotsBrand]: string }
type Slots = { readonly [translationSlotsBrand]: Brand };
export type { Brand, Template, Slots };
translationSlotsBrand.toString();
