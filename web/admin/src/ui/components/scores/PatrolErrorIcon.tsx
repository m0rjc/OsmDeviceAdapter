import { useState, useEffect, useRef } from 'react';

interface PatrolErrorIconProps {
    errorMessage?: string;
    retryAfter?: number;
}

/**
 * Displays an error icon for a patrol with a tooltip showing error details.
 *
 * - Orange warning triangle: Temporary errors (retryAfter > 0) that will auto-retry
 * - Red error hexagon: Permanent errors (retryAfter === -1) requiring force sync
 *
 * Tooltip shows on hover (desktop) and click/touch (mobile).
 */
export function PatrolErrorIcon({ errorMessage, retryAfter }: PatrolErrorIconProps) {
    const [showTooltip, setShowTooltip] = useState(false);
    const [retryTimeLeft, setRetryTimeLeft] = useState<string | null>(null);
    const [tooltipPosition, setTooltipPosition] = useState({ top: 0, left: 0 });
    const tooltipRef = useRef<HTMLDivElement>(null);
    const iconRef = useRef<HTMLSpanElement>(null);

    // Determine icon type: temporary (triangle) or permanent (hexagon)
    const isPermanent = retryAfter === -1;
    const isTemporary = retryAfter !== undefined && retryAfter > 0;

    // Update countdown timer for temporary errors
    useEffect(() => {
        if (!isTemporary || retryAfter === undefined) {
            setRetryTimeLeft(null);
            return;
        }

        const updateCountdown = () => {
            const now = Date.now();
            const msLeft = retryAfter - now;

            if (msLeft <= 0) {
                setRetryTimeLeft('Ready to retry');
                return;
            }

            const seconds = Math.ceil(msLeft / 1000);
            const minutes = Math.floor(seconds / 60);
            const remainingSeconds = seconds % 60;

            if (minutes > 0) {
                setRetryTimeLeft(`${minutes}m ${remainingSeconds}s`);
            } else {
                setRetryTimeLeft(`${remainingSeconds}s`);
            }
        };

        updateCountdown();
        const interval = setInterval(updateCountdown, 1000);

        return () => clearInterval(interval);
    }, [isTemporary, retryAfter]);

    // Update tooltip position when shown
    useEffect(() => {
        if (!showTooltip || !iconRef.current) return;

        const updatePosition = () => {
            if (!iconRef.current) return;

            const iconRect = iconRef.current.getBoundingClientRect();
            const tooltipWidth = 250; // max-width from CSS
            const tooltipHeight = 100; // approximate height
            const gap = 8; // gap between icon and tooltip

            // Calculate position (centered above the icon)
            let left = iconRect.left + iconRect.width / 2 - tooltipWidth / 2;
            let top = iconRect.top - tooltipHeight - gap;

            // Keep tooltip within viewport bounds
            const viewportWidth = window.innerWidth;

            // Adjust horizontal position if it goes off screen
            if (left < 10) {
                left = 10;
            } else if (left + tooltipWidth > viewportWidth - 10) {
                left = viewportWidth - tooltipWidth - 10;
            }

            // If tooltip would go above viewport, show it below instead
            if (top < 10) {
                top = iconRect.bottom + gap;
            }

            setTooltipPosition({ top, left });
        };

        updatePosition();

        // Update position on scroll/resize
        window.addEventListener('scroll', updatePosition, true);
        window.addEventListener('resize', updatePosition);

        return () => {
            window.removeEventListener('scroll', updatePosition, true);
            window.removeEventListener('resize', updatePosition);
        };
    }, [showTooltip]);

    // Close tooltip when clicking outside
    useEffect(() => {
        if (!showTooltip) return;

        const handleClickOutside = (event: MouseEvent) => {
            if (
                tooltipRef.current &&
                iconRef.current &&
                !tooltipRef.current.contains(event.target as Node) &&
                !iconRef.current.contains(event.target as Node)
            ) {
                setShowTooltip(false);
            }
        };

        document.addEventListener('mousedown', handleClickOutside);
        return () => document.removeEventListener('mousedown', handleClickOutside);
    }, [showTooltip]);

    const handleClick = (e: React.MouseEvent) => {
        e.stopPropagation();
        setShowTooltip(!showTooltip);
    };

    const handleKeyDown = (e: React.KeyboardEvent) => {
        if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            setShowTooltip(!showTooltip);
        }
    };

    // Triangle SVG for temporary errors
    const TriangleIcon = () => (
        <svg
            width="16"
            height="16"
            viewBox="0 0 16 16"
            fill="none"
            xmlns="http://www.w3.org/2000/svg"
            aria-hidden="true"
        >
            <path
                d="M8 2L14.928 14H1.072L8 2Z"
                fill="var(--color-warning)"
                stroke="var(--color-warning)"
                strokeWidth="1"
            />
            <text
                x="8"
                y="12"
                textAnchor="middle"
                fontSize="10"
                fontWeight="bold"
                fill="white"
            >
                !
            </text>
        </svg>
    );

    // Hexagon SVG for permanent errors
    const HexagonIcon = () => (
        <svg
            width="16"
            height="16"
            viewBox="0 0 16 16"
            fill="none"
            xmlns="http://www.w3.org/2000/svg"
            aria-hidden="true"
        >
            <path
                d="M8 1L13.464 4.5V11.5L8 15L2.536 11.5V4.5L8 1Z"
                fill="var(--color-danger)"
                stroke="var(--color-danger)"
                strokeWidth="1"
            />
            <text
                x="8"
                y="12"
                textAnchor="middle"
                fontSize="10"
                fontWeight="bold"
                fill="white"
            >
                !
            </text>
        </svg>
    );

    return (
        <span
            ref={iconRef}
            className="patrol-error-icon"
            onClick={handleClick}
            onKeyDown={handleKeyDown}
            role="button"
            tabIndex={0}
            aria-label={`Error: ${errorMessage}`}
        >
            {isPermanent && <HexagonIcon />}
            {isTemporary && <TriangleIcon />}

            {showTooltip && (
                <div
                    ref={tooltipRef}
                    className="patrol-error-tooltip"
                    style={{
                        top: `${tooltipPosition.top}px`,
                        left: `${tooltipPosition.left}px`,
                    }}
                >
                    <div className="patrol-error-tooltip-message">
                        {errorMessage || 'Unknown error'}
                    </div>
                    {isTemporary && retryTimeLeft && (
                        <div className="patrol-error-tooltip-retry">
                            Retry in: {retryTimeLeft}
                        </div>
                    )}
                    {isPermanent && (
                        <div className="patrol-error-tooltip-action">
                            Use Force Sync to retry
                        </div>
                    )}
                </div>
            )}
        </span>
    );
}
