/** Dutch greeting for the local hour: 00–06 nacht, 06–12 morgen, 12–18 middag, 18–24 avond. */
export function greeting(date: Date): string {
	const h = date.getHours();
	if (h < 6) return 'Goedenacht';
	if (h < 12) return 'Goedemorgen';
	if (h < 18) return 'Goedemiddag';
	return 'Goedenavond';
}

export interface NameParts {
	username: string;
	display_name?: string | null;
	given_name?: string | null;
}

/** AD givenName, falling back to displayName, then the username. */
export function firstName(me: NameParts): string {
	return me.given_name?.trim() || me.display_name?.trim() || me.username;
}
