// A module written to hold, in one file, the constructs real application code mixes: components and hooks reading
// nested properties, every loop and branch form, destructuring with holes and defaults, classes with private and
// computed members, generators, async functions, JSX with fragments and whitespace, and closures over all of it.
import { useCallback, useEffect, useMemo, useState } from 'react';

export enum Status {
    Idle,
    Loading,
    Ready,
}

export interface Account {
    identifier: string;
    profile: { name: string; address: { city: string; country?: string } };
    roles: string[];
    balance?: number;
}

export type Reading = [label: string, value: number];

const inspectKey = Symbol.for('dashboard.inspect');

export class Ledger {
    static #instances = 0;
    readonly #entries: Reading[] = [];

    static {
        Ledger.#instances = 0;
    }

    constructor(private readonly owner: Account) {
        Ledger.#instances += 1;
    }

    get size(): number {
        return this.#entries.length;
    }

    set latest(reading: Reading) {
        this.#entries.push(reading);
    }

    [inspectKey](): string {
        return `Ledger of ${this.owner.profile.name}`;
    }

    get [Symbol.toStringTag](): string {
        return 'Ledger';
    }

    *readings(): Generator<Reading> {
        for (const entry of this.#entries) {
            yield entry;
        }
    }

    async total(): Promise<number> {
        let sum = 0;
        for await (const [, value] of this.readings()) {
            sum += value;
        }
        return sum;
    }
}

export function summarize(readings: Reading[], fallback = 0, ...extra: number[]): number {
    const [first, , third = fallback, ...rest] = readings;
    let total = (first?.[1] ?? fallback) + (third?.[1] ?? 0);
    let skipped: Reading | undefined;
    [, skipped] = readings;
    outer: for (let index = 0; index < rest.length; index++) {
        for (const key in rest[index]) {
            if (key === 'length') {
                continue outer;
            }
            if (Number(key) > 3) {
                break outer;
            }
        }
        total += rest[index]![1];
    }
    let attempts = 0;
    do {
        attempts++;
    } while (attempts < extra.length);
    while (attempts > 0) {
        attempts -= 2;
    }
    switch (true) {
        case total > 100:
            total = 100;
            break;
        case total < 0: {
            total = 0;
        }
        // falls through
        default:
            total = Math.round(total);
    }
    try {
        total = total / (skipped ? skipped[1] : 1);
    } catch (error) {
        total = error instanceof RangeError ? -1 : total;
    } finally {
        attempts = 0;
    }
    return total satisfies number;
}

export function useAccount(account: Account | undefined, refresh: () => Promise<Account>) {
    const [status, setStatus] = useState(Status.Idle);
    const city = account?.profile.address.city ?? 'unknown';
    const label = useMemo(() => `${account?.profile.name} in ${city}`, [account?.profile.name, city]);
    const reload = useCallback(async () => {
        setStatus(Status.Loading);
        const fresh = await refresh();
        setStatus(fresh.roles.includes('admin') ? Status.Ready : Status.Idle);
        return fresh;
    }, [refresh]);
    useEffect(() => {
        const timer = setTimeout(() => void reload(), 1000);
        return () => clearTimeout(timer);
    }, [reload]);
    const stream = {
        async *[Symbol.asyncIterator]() {
            yield status;
        },
        get [inspectKey]() {
            return label;
        },
    };
    return { status, label, reload, stream };
}

export function AccountCard(properties: { account: Account; onSelect?: (identifier: string) => void }) {
    const { account, onSelect } = properties;
    const { profile: { name, address: { city, country = 'nowhere' } }, roles: [primaryRole] } = account;
    const handleClick = () => onSelect?.(account.identifier);
    const balance = account.balance !== undefined ? account.balance.toFixed(2) : null;
    return (
        <section className="card" onClick={handleClick} data-role={primaryRole}>
            <h2>{name}</h2>
            <p>
                {city}, {country}
            </p>
            {balance && <span className="balance">{balance}</span>}
            {/* the roles, one per line */}
            <ul>
                {account.roles.map((role, index) => (
                    <li key={role}>
                        {index + 1}. {role}
                    </li>
                ))}
            </ul>
        </section>
    );
}

export function Dashboard(properties: { accounts: Account[]; title: string; refresh: () => Promise<Account> }) {
    const { status, label, reload } = useAccount(properties.accounts[0], properties.refresh);
    const sorted = useMemo(
        () => [...properties.accounts].sort((left, right) => left.profile.name.localeCompare(right.profile.name)),
        [properties.accounts],
    );
    const summary = (() => summarize(sorted.map((account): Reading => [account.identifier, account.balance ?? 0])))();
    return (
        <>
            <header>
                <h1>{properties.title}</h1>
                <button onClick={() => void reload()} disabled={status === Status.Loading}>
                    Reload {label}
                </button>
            </header>
            {sorted.length === 0 ? (
                <p>No accounts</p>
            ) : (
                sorted.map((account) => <AccountCard key={account.identifier} account={account} />)
            )}
            <footer>{summary}</footer>
        </>
    );
}
