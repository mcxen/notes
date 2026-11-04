/** Local record declarations. This module has no host or network integration. */
export type RichText = ({ text: string; marks?: Record<string, string | number | boolean | Record<string, unknown>> } | { embed: Record<string, unknown>; marks?: Record<string, string | number | boolean | Record<string, unknown>> })[];
export class Field<T, Optional extends boolean = false> {
  declare readonly value: T;
  declare readonly isOptional: Optional;
  optional(): Field<T, true> { return new Field<T, true>(); }
  describe(_text: string): this { return this; }
}
type Fields = Record<string, Field<unknown, boolean>>;
type Value<F> = F extends Field<infer T, boolean> ? T : never;
export type ObjectValue<F extends Fields> = {
  [K in keyof F as F[K] extends Field<unknown, true> ? never : K]: Value<F[K]>;
} & { [K in keyof F as F[K] extends Field<unknown, true> ? K : never]?: Value<F[K]> };
export const t = {
  string: (_max?: number) => new Field<string>(),
  bool: () => new Field<boolean>(),
  int: (_min?: number, _max?: number) => new Field<number>(),
  number: (_min?: number, _max?: number) => new Field<number>(),
  choice: <const T extends readonly string[]>(..._choices: T) => new Field<T[number]>(),
  list: <T>( _item: Field<T, boolean>, _max?: number) => new Field<T[]>(),
  object: <F extends Fields>(_fields: F) => new Field<ObjectValue<F>>(),
  json: (_schema: Record<string, unknown>) => new Field<Record<string, unknown>>(),
  richText: () => new Field<RichText>(),
};
export interface CollectionHandle<T> { readonly recordType?: T }
type Collection = { fields: Fields; [key: string]: unknown };
export function schema<C extends Record<string, Collection>>(declaration: { collections: C }): { [K in keyof C]: CollectionHandle<ObjectValue<C[K]['fields']>> } {
  return Object.fromEntries(Object.keys(declaration.collections).map(name => [name, {}])) as { [K in keyof C]: CollectionHandle<ObjectValue<C[K]['fields']>> };
}
