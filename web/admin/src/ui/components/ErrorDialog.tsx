import { shallowEqual } from 'react-redux';
import { useAppDispatch, useAppSelector } from '../state/hooks';
import { closeErrorDialog, selectDialogState } from '../state';

/**
 * Error dialog component for displaying service errors.
 *
 * This dialog is controlled by Redux state and can be triggered
 * by dispatching showErrorDialog action from anywhere in the app.
 *
 * Typical use case: ServiceErrorMessage from worker (e.g., "Failed to refresh scores")
 *
 * Note: This is a modal dialog for transient errors that can be dismissed.
 * For persistent/critical errors that block the UI, see MessageCard component.
 */
export function ErrorDialog() {
  const dispatch = useAppDispatch();
  const { isErrorDialogOpen, errorTitle, errorMessage } = useAppSelector(
    selectDialogState,
    shallowEqual
  );

  if (!isErrorDialogOpen) {
    return null;
  }

  const handleClose = () => {
    dispatch(closeErrorDialog());
  };

  return (
    <div className="modal-overlay" onClick={handleClose}>
      <div className="modal" onClick={e => e.stopPropagation()}>
        <div className="modal-header">
          <h3>{errorTitle || 'Error'}</h3>
        </div>
        <div className="modal-body">
          <p style={{ color: 'var(--color-text)' }}>{errorMessage}</p>
        </div>
        <div className="modal-footer">
          <button
            className="btn btn-primary"
            onClick={handleClose}
          >
            OK
          </button>
        </div>
      </div>
    </div>
  );
}
