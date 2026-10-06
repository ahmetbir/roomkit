/** num/fixed; `decimalComma()` says whether the current language writes a decimal comma. */
export declare function makeFormat(decimalComma: () => boolean): {
    num(v: number, digits?: number): string;
    fixed(v: number, digits: number): string;
};
