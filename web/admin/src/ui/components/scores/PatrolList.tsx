import {PatrolCard} from './PatrolCard';

interface PatrolListProps {
    /** Array of patrols to display */
    patrolKeys: string[];
    sectionError?: string;
}

/**
 * Renders a list of patrol score cards.
 *
 * Displays an empty state message if no patrols are available.
 * Otherwise renders a PatrolCard for each patrol in the array.
 */
export function PatrolList({patrolKeys, sectionError}: PatrolListProps) {
    if (patrolKeys.length === 0) {
        return (
            <div className="empty-state">
                <h3>No Patrols Found</h3>
                <p>{sectionError || 'This section doesn\'t have any patrols configured.'}</p>
            </div>
        );
    }

    return (
        <div className="patrol-cards">
            {patrolKeys.map(patrol =>
                <PatrolCard
                    key={patrol}
                    patrolId={patrol}
                />
            )}
        </div>
    );
}
