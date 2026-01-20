import { useState, useRef, useEffect } from 'react';

interface MenuOption {
  label: string;
  onClick?: () => void;
  disabled?: boolean;
  href?: string; // For external links
}

interface MenuProps {
  options: MenuOption[];
}

export function Menu({ options }: MenuProps) {
  const [isOpen, setIsOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);

  // Close menu when clicking outside
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(event.target as Node)) {
        setIsOpen(false);
      }
    };

    if (isOpen) {
      document.addEventListener('mousedown', handleClickOutside);
    }

    return () => {
      document.removeEventListener('mousedown', handleClickOutside);
    };
  }, [isOpen]);

  const handleOptionClick = (option: MenuOption) => {
    if (option.href) {
      window.open(option.href, '_blank', 'noopener,noreferrer');
    } else if (option.onClick) {
      option.onClick();
    }
    setIsOpen(false);
  };

  return (
    <div className="menu-container" ref={menuRef}>
      <button
        className="btn btn-secondary menu-button"
        onClick={() => setIsOpen(!isOpen)}
        aria-label="More options"
        aria-expanded={isOpen}
      >
        ⋮
      </button>
      {isOpen && (
        <div className="menu-dropdown">
          {options.map((option, index) => (
            <button
              key={index}
              className="menu-item"
              onClick={() => handleOptionClick(option)}
              disabled={option.disabled}
            >
              {option.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
