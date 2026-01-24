import { useAppDispatch, useAppSelector } from '../state/hooks';
import {
  selectSections,
  selectSelectedSectionId,
  selectSection,
} from '../state';

/**
 * Dropdown selector for choosing a section.
 *
 * Automatically hides when there is only one section available (no choice needed).
 * Updates Redux state when selection changes, which triggers score loading.
 *
 * The selector displays sections in "Group Name - Section Name" format.
 */
export function SectionSelector() {
  const dispatch = useAppDispatch();
  const sections = useAppSelector(selectSections);
  const selectedSectionId = useAppSelector(selectSelectedSectionId);

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
        onChange={e => dispatch(selectSection(Number(e.target.value)))}
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
