/*
 * Node's RegExp answers what the regexp package's property tests hold esregexp to (#3jt7qa6).
 *
 * Reads one JSON object on standard input, from main.go beside this file:
 *
 *   - Unicode: the Unicode version the property tables were generated from, as Node spells it in
 *     process.versions.unicode; a Node on another version refuses, since its answers would be for other tables;
 *   - Properties: each property spelling and the code points to ask about;
 *   - Fixtures and Subjects: the property escape patterns, and what each is tested against.
 *
 * Writes one JSON object on standard output: Node's version and Unicode version, and for each spelling whether
 * `\p{spelling}` compiles under `u` and, when it does, one string of 0 and 1 per profile, a character per code
 * point, in the order main.go reads them: flags `u` then `iu`, escape `p` then `P`, and the escape bare, inside a
 * class, then inside a negated class. For each fixture, whether it compiles and, when it does, whether it matches
 * each subject.
 *
 * Nothing runs this but main.go, and nothing runs main.go but a person regenerating the recordings.
 */

import * as NodeFileSystem from 'node:fs';

const input = JSON.parse(NodeFileSystem.readFileSync(0, 'utf8'));
if(process.versions.unicode !== input.Unicode) {
    throw new Error(
        'Node ' + process.version + ' has Unicode ' + process.versions.unicode + '; the tables want ' + input.Unicode,
    );
}

function propertySource(name, escape, shape) {
    const atom = '\\' + escape + '{' + name + '}';
    if(shape === 1) {
        return '^(?:[' + atom + '])$';
    }
    if(shape === 2) {
        return '^(?:[^' + atom + '])$';
    }
    return '^(?:' + atom + ')$';
}

const properties = input.Properties.map(function(property) {
    try {
        new RegExp('\\p{' + property.Name + '}', 'u');
    }
    catch {
        return { Name: property.Name, Accepted: false, Bits: null };
    }
    const bits = [];
    for(const flags of ['u', 'iu']) {
        for(const escape of ['p', 'P']) {
            for(const shape of [0, 1, 2]) {
                const expression = new RegExp(propertySource(property.Name, escape, shape), flags);
                bits.push(
                    property.Points.map(function(point) {
                        return expression.test(String.fromCodePoint(point)) ? '1' : '0';
                    }).join(''),
                );
            }
        }
    }
    return { Name: property.Name, Accepted: true, Bits: bits };
});

const fixtures = input.Fixtures.map(function(fixture) {
    let expression;
    try {
        expression = new RegExp(fixture.Source, fixture.Flags);
    }
    catch {
        return { Accepted: false, Matches: null };
    }
    return {
        Accepted: true,
        Matches: input.Subjects.map(function(subject) {
            return expression.test(subject);
        }),
    };
});

process.stdout.write(
    JSON.stringify({
        Node: process.version,
        Unicode: process.versions.unicode,
        Properties: properties,
        Fixtures: fixtures,
    }),
);
