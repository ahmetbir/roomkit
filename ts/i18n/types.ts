// Dictionary types shared by the Turkish source and its translations.

/** A count-dependent text: Intl.PluralRules picks the form from params.n. */
export type Plural = { readonly one: string; readonly other: string };
export type Msg = string | Plural;

/**
 * A translation of a Turkish area: exactly its keys (an annotated object
 * literal fails to compile on a missing or an extra key).
 */
export type Area<T> = { readonly [K in keyof T]: Msg };

export type Params = Readonly<Record<string, string | number>>;
