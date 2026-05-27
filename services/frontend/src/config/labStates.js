import {
  ClockIcon,
  ArrowPathIcon,
  CheckCircleIcon,
  BoltIcon,
  BoltSlashIcon,
  ExclamationTriangleIcon,
} from '@heroicons/vue/24/outline'

export { ClockIcon, BoltSlashIcon, ExclamationTriangleIcon }

export const attemptConfig = {
  new:             { label: 'Starting',      dot: 'bg-slate-400',  text: 'text-slate-400',  ping: false },
  provisioning:    { label: 'Provisioning',  dot: 'bg-yellow-400', text: 'text-yellow-400', ping: true  },
  provisioned:     { label: 'Running',       dot: 'bg-green-400',  text: 'text-green-400',  ping: true  },
  decommissioning: { label: 'Shutting down', dot: 'bg-orange-400', text: 'text-orange-400', ping: true  },
}

export const serverStateConfig = {
  pending:         { icon: ClockIcon,       iconCls: 'text-slate-500',  label: 'Pending',      labelCls: 'text-slate-500'  },
  provisioning:    { icon: ArrowPathIcon,   iconCls: 'text-yellow-400', label: 'Provisioning', labelCls: 'text-yellow-400', spin: true },
  provisioned:     { icon: CheckCircleIcon, iconCls: 'text-green-400',  label: 'Ready',        labelCls: 'text-green-400'  },
  decommissioning: { icon: ArrowPathIcon,   iconCls: 'text-orange-400', label: 'Stopping',     labelCls: 'text-orange-400', spin: true },
}

export const serverStatusConfig = {
  running:  { icon: BoltIcon,      iconCls: 'text-green-400',  label: 'On',      labelCls: 'text-green-400'  },
  stopped:  { icon: BoltSlashIcon, iconCls: 'text-slate-500',  label: 'Off',     labelCls: 'text-slate-500'  },
  booting:  { icon: ArrowPathIcon, iconCls: 'text-yellow-400', label: 'Booting', labelCls: 'text-yellow-400', spin: true },
}
