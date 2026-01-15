import { useAuth } from '../hooks';

export function SectionSelector() {
  const { sections, selectedSectionId, setSelectedSectionId } = useAuth();

  if (sections.length <= 1) {
    // Don't show selector if only one section
    return null;
  }

  return (
    <div className="section-selector">
      <label htmlFor="section-select">Section</label>
      <select
        id="section-select"
        value={selectedSectionId ?? ''}
        onChange={e => setSelectedSectionId(Number(e.target.value))}
      >
        {sections.map(section => (
          <option key={section.id} value={section.id}>
            {section.groupName} - {section.name}
          </option>
        ))}
      </select>
    </div>
  );
}
